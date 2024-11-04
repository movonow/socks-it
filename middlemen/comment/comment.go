package comment

import (
	"bytes"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"math"
	"net/http"
	"net/url"
	"socks.it/proxy"
	"socks.it/proxy/decorators"
	"socks.it/utils/errs"
	"strings"
	"sync/atomic"
	"time"
)

var commentServeURL = flag.String("commentServeURL", "http://localhost:10081/", "Commentable web server url")
var readTopic = flag.String("readTopic", "How are you?", "Read comment from this topic")
var writeTopic = flag.String("writeTopic", "I am fine.", "Write comment to this topic")

type Topic struct {
	ID    int    `json:"id"`
	Title string `json:"title"`
}

type Comment struct {
	ID int `json:"id,omitempty"`
	//Author  string    `json:"author"`
	Content string `json:"content,omitempty"`
	//Created time.Time `json:"created"`
}

// commentMiddleman continuously polls for new requests and performs the operations
type commentMiddleman struct {
	logger      *slog.Logger
	rawProxyURL string
}

type Option func(*commentMiddleman)

func WithProxy(rawProxyURL string) Option {
	return func(m *commentMiddleman) {
		m.rawProxyURL = rawProxyURL
	}
}

func New(logger *slog.Logger, options ...Option) proxy.Middleman {
	for strings.HasSuffix(*commentServeURL, "/") {
		*commentServeURL = strings.TrimRight(*commentServeURL, "/")
	}
	m := commentMiddleman{
		logger: logger,
	}

	for _, option := range options {
		option(&m)
	}

	return &m
}

func (m *commentMiddleman) Setup() error {
	return nil
}

func (m *commentMiddleman) Teardown() error {
	return nil
}

func (m *commentMiddleman) NewTransport() (proxy.Transporter, error) {
	httpTransport := http.Transport{
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}
	if m.rawProxyURL != "" {
		p, err := url.Parse(m.rawProxyURL)
		if err != nil {
			m.logger.Error("failed to parse url", "error", err)
			return nil, errs.WithStack(err)
		}
		httpTransport.Proxy = http.ProxyURL(p)
	}

	t := &commentTransport{
		logger:        m.logger,
		rawProxyURL:   m.rawProxyURL,
		readCommentID: -1,
		client: &http.Client{
			Transport: &httpTransport,
			Timeout:   10 * time.Second,
		},
	}

	getOrCreateTopic := func(title string) (int, error) {
		id, err := t.getTopic(title)
		if id == 0 {
			if err != nil {
				t.logger.Info("failed to get topic", "title", title, "error", err)
				return 0, err
			}
			// 404
			id, err = t.createTopic(title)
			if err != nil {
				return 0, err
			}
		}
		return id, nil
	}

	var err error
	t.readTopicID, err = getOrCreateTopic(*readTopic)
	if err != nil {
		return nil, err
	}

	t.writeTopicID, err = getOrCreateTopic(*writeTopic)
	if err != nil {
		return nil, err
	}

	t.readChan = make(chan io.Reader, 512)
	t.errChan = make(chan error, 1)

	go t.pollComments(t.readTopicID)
	return decorators.NewBase64Transport(t, t.logger), nil
}

func (m *commentMiddleman) WriteSpace() int {
	return 8192 * 3 / 4 // base64 encoded
}

type commentTransport struct {
	logger      *slog.Logger
	rawProxyURL string
	client      *http.Client

	readTopicID   int
	readCommentID int
	writeTopicID  int
	readChan      chan io.Reader
	errChan       chan error
	closed        atomic.Bool
}

func (t *commentTransport) NextWriter() (io.WriteCloser, error) {
	w := commentWriter{
		commentTransport: t,
	}
	return &w, nil
}

type commentWriter struct {
	*commentTransport
	bytes.Buffer
}

func (c *commentWriter) Close() error {
	err := c.commentTransport.postComment(c.writeTopicID, c.String())
	if err != nil {
		c.errChan <- err //fixme: 通知Reader关闭，如果是网络问题，那么poll应该能检测到err
	}
	return err
}

func (t *commentTransport) NextReader() (io.Reader, error) {
	select {
	case err := <-t.errChan:
		return nil, err
	case r := <-t.readChan:
		return r, nil
	}
}

func (t *commentTransport) Close() error {
	t.closed.Store(true)
	select {
	case <-t.errChan: // poll routine exited.
	default:
	}
	close(t.readChan)
	close(t.errChan)
	return nil
}

func (t *commentTransport) getTopic(title string) (int, error) {
	resp, err := t.client.Get(fmt.Sprintf("%s/topic?title=%s", *commentServeURL, url.QueryEscape(title)))
	if err != nil {
		t.logger.Error("failed to get topic", "title", title, "error", err)
		return 0, errs.WithStack(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		if resp.StatusCode != http.StatusNotFound {
			return 0, fmt.Errorf("query topic failed with status code %d", resp.StatusCode)
		}
		return 0, nil
	}

	var body = new(Topic)
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, errs.WithStack(err)
	}

	return body.ID, nil
}

func (t *commentTransport) createTopic(title string) (int, error) {
	topic := Topic{
		Title: title,
	}
	body, err := json.Marshal(topic)
	if err != nil {
		return 0, errs.WithStack(err)
	}

	resp, err := t.client.Post(fmt.Sprintf("%s/topic/create", *commentServeURL), "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, errs.WithStack(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("create topic failed with status code %d", resp.StatusCode)
	}

	if err := json.NewDecoder(resp.Body).Decode(&topic); err != nil {
		return 0, errs.WithStack(err)
	}

	return topic.ID, nil
}

func (t *commentTransport) postComment(topicID int, content string) error {
	comment := Comment{
		Content: content,
	}
	body, err := json.Marshal(comment)
	if err != nil {
		return errs.WithStack(err)
	}

	resp, err := t.client.Post(fmt.Sprintf("%s/topic/comment?topic=%d", *commentServeURL, topicID), "application/json", bytes.NewReader(body))
	if err != nil {
		return err
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send request to discussion server: %s", resp.Status)
	}

	return nil
}

func (t *commentTransport) getLatestCommentID(topicID int) (int, error) {
	resp, err := t.client.Get(fmt.Sprintf("%s/topic/comments/latest?topic=%d", *commentServeURL, topicID))
	if err != nil {
		return 0, errs.WithStack(err)
	}
	defer func(Body io.ReadCloser) {
		_ = Body.Close()
	}(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return 0, fmt.Errorf("query comment id failed with status code %d", resp.StatusCode)
	}

	var body = struct {
		LatestCommentID int `json:"latest_comment_id"`
	}{}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return 0, errs.WithStack(err)
	}

	return body.LatestCommentID, nil
}

func (t *commentTransport) pollComments(topicID int) {
	const maxWait = 100.0
	for {
		if t.closed.Load() {
			t.errChan <- errors.New("discussion server closed")
			break
		}

		var wait = maxWait
		err := func() error {
			latestID, err := t.getLatestCommentID(topicID)
			if err != nil {
				t.errChan <- err
			}

			if t.readCommentID == latestID {
				return nil
			}

			// Update id on startup, don't read remnant.
			if t.readCommentID == -1 {
				t.logger.Debug("initialize read comment", "id", latestID)
				t.readCommentID = latestID
				return nil
			}

			resp, err := t.client.Get(fmt.Sprintf("%s/topic/comments/range?topic=%d&start=%d&end=%d", *commentServeURL, topicID, t.readCommentID, latestID))
			if err != nil {
				return errs.WithStack(err)
			}
			defer func(Body io.ReadCloser) {
				_ = Body.Close()
			}(resp.Body)

			if resp.StatusCode != http.StatusOK {
				return errs.WithStack(fmt.Errorf("failed to get comments: %s", resp.Status))
			}

			var comments []Comment
			if err := json.NewDecoder(resp.Body).Decode(&comments); err != nil {
				return errs.WithStack(err)
			}

			for _, comment := range comments {
				wait -= 5
				if t.readCommentID != comment.ID { // open range: [start,end], comment ID can duplicate.
					t.readChan <- strings.NewReader(comment.Content)
				}
			}
			t.readCommentID = latestID
			return nil
		}()
		if err != nil {
			t.logger.Error("poll failed", "error", err)
			t.errChan <- err
			break
		}

		time.Sleep(time.Duration(math.Max(0, wait)) * time.Millisecond) // Polling interval
	}
}
