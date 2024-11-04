package main

import (
	"bytes"
	"container/ring"
	"encoding/json"
	"flag"
	"fmt"
	"github.com/tebeka/atexit"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"socks.it/utils/logs"
	"strconv"
	"sync"
	"syscall"
	"time"
)

const maxComments = 512 * 1024 // Max number of comments per topic

var commentServeAddr = flag.String("commentServeAddr", "localhost:10081", "Commentable web server url")

// Comment represents a comment on a discussion topic
type Comment struct {
	ID      int       `json:"id"`
	Author  string    `json:"author"`
	Content string    `json:"content,omitempty"`
	Created time.Time `json:"created"`
}

// Topic represents a discussion topic with a ring buffer of comments
type Topic struct {
	ID              int        `json:"id"`
	Title           string     `json:"title"`
	Content         string     `json:"content"`
	Created         time.Time  `json:"created"`
	Comments        *ring.Ring `json:"-"` // Circular buffer for comments
	LatestCommentID int        `json:"-"` // The latest comment ID in the topic
	TotalComments   int        `json:"-"` // Total comments added to the topic
}

// In-memory storage for the topics and comments
var (
	topics = make(map[int]*Topic)
	mu     sync.Mutex
	logger *slog.Logger
)

// createTopicHandler handles the creation of new topics
func createTopicHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logger.Warn("Method not allowed", "method", r.Method, "require", http.MethodPost)
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	var topic Topic
	logBuffer := new(bytes.Buffer)
	teeReader := io.TeeReader(r.Body, logBuffer)
	err := json.NewDecoder(teeReader).Decode(&topic)
	if err != nil {
		logger.Warn("Error decoding JSON", "error", err, "body", logBuffer.String())
		http.Error(w, "Invalid request payload", http.StatusBadRequest)
		return
	}

	// Assign a unique ID and set the creation time
	mu.Lock()

	for _, item := range topics {
		if item.Title == topic.Title {
			mu.Unlock()
			http.Error(w, "Topic already exists", http.StatusConflict)
			return
		}
	}

	topic.ID = len(topics) + 1
	topic.Created = time.Now()
	topic.Comments = ring.New(maxComments) // Initialize the ring buffer for comments
	topic.LatestCommentID = 0              // Initialize LatestCommentID for the topic
	topic.TotalComments = 0                // Initialize total comments count
	topics[topic.ID] = &topic
	mu.Unlock()

	// Send back the created topic as the response
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(topic); err != nil {
		//http.Error(w, "create topic failed", http.StatusInternalServerError)
		logger.Error("create topic failed", "error", err)
		return
	}
}

func getTopicHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Warn("Method not allowed", "method", r.Method, "require", http.MethodGet)
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse the topic ID from the URL query
	topicTitleStr := r.URL.Query().Get("title")
	if topicTitleStr == "" {
		logger.Warn("Topic title is required", "url", r.URL.RawQuery)
		http.Error(w, "title is required", http.StatusBadRequest)
		return
	}

	var topic *Topic
	mu.Lock()
	for _, item := range topics {
		if item.Title == topicTitleStr {
			topic = item
		}
	}
	mu.Unlock()

	if topic == nil {
		http.Error(w, "topic not found", http.StatusNotFound)
		return
	}

	// Send the topic as the response
	w.Header().Set("Content-Type", "application/json")
	err := json.NewEncoder(w).Encode(topic)
	if err != nil {
		logger.Error("fetch topic failed", "error", err)
		return
	}
}

func addCommentHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		logger.Warn("Method not allowed", "method", r.Method, "require", http.MethodPost)
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse the topic ID from the URL query
	topicIDStr := r.URL.Query().Get("topic")
	if topicIDStr == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	topicID, _ := strconv.Atoi(topicIDStr)

	var comment Comment
	if err := func() error {
		mu.Lock()
		defer mu.Unlock()

		topic, exists := topics[topicID]
		if !exists {
			http.Error(w, "Topic not found", http.StatusNotFound)
			return fmt.Errorf("topic %d not found", topicID)
		}

		// Decode the comment from the request body
		err := json.NewDecoder(r.Body).Decode(&comment)
		if err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return err
		}

		comment.ID = topic.LatestCommentID + 1
		comment.Created = time.Now()

		// Add the comment to the ring buffer (overwriting oldest if full)
		topic.Comments.Value = comment
		topic.Comments = topic.Comments.Next()

		topic.LatestCommentID++
		topic.TotalComments++

		topics[topicID] = topic

		return nil
	}(); err != nil {
		return
	}

	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(Comment{
		ID:     topicID,
		Author: r.RemoteAddr,
		//Content: comment.Content,
		Created: comment.Created,
	}); err != nil {
		logger.Error("create comment failed", "error", err)
		return
	}
}

// getCommentsByIDRangeHandler fetches a batch of comments by a specified ID range
func getCommentsByIDRangeHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Warn("Method not allowed", "method", r.Method, "require", http.MethodGet)
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse the topic ID and comment ID range from the query parameters
	topicIDStr := r.URL.Query().Get("topic")
	startIDStr := r.URL.Query().Get("start")
	endIDStr := r.URL.Query().Get("end")
	if topicIDStr == "" || startIDStr == "" || endIDStr == "" {
		http.Error(w, "Topic ID, start ID, and end ID are required", http.StatusBadRequest)
		return
	}

	topicID, _ := strconv.Atoi(topicIDStr)
	startID, _ := strconv.Atoi(startIDStr)
	endID, _ := strconv.Atoi(endIDStr)

	mu.Lock()
	topic, exists := topics[topicID]
	mu.Unlock()

	if !exists {
		http.Error(w, "Topic not found", http.StatusNotFound)
		return
	}

	// Collect comments within the ID range
	var comments []Comment
	topic.Comments.Do(func(c interface{}) {
		if c != nil {
			comment := c.(Comment)
			if comment.ID >= startID && comment.ID <= endID {
				comments = append(comments, comment)
			}
		}
	})

	// Send the filtered comments as the response
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(comments); err != nil {
		logger.Error("fetch comments failed", "error", err)
		return
	}
}

// getLatestCommentIDHandler returns the latest comment ID for a specific topic
func getLatestCommentIDHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		logger.Warn("Method not allowed", "method", r.Method, "require", http.MethodGet)
		http.Error(w, "Invalid request method", http.StatusMethodNotAllowed)
		return
	}

	// Parse the topic ID from the URL query
	topicIDStr := r.URL.Query().Get("topic")
	if topicIDStr == "" {
		http.Error(w, "ID is required", http.StatusBadRequest)
		return
	}

	topicID, _ := strconv.Atoi(topicIDStr)

	mu.Lock()
	topic, exists := topics[topicID]
	mu.Unlock()

	if !exists {
		http.Error(w, "Topic not found", http.StatusNotFound)
		return
	}

	// Send the latest comment ID as the response
	w.Header().Set("Content-Type", "application/json")

	if err := json.NewEncoder(w).Encode(map[string]int{"latest_comment_id": topic.LatestCommentID}); err != nil {
		logger.Error("fetch comments failed", "error", err)
		return
	}
}

func main() {
	flag.Parse()

	logger = logs.GetLogger("run/app.log", "Debug")

	atexit.Register(func() { logger.Info("Shutting down...") })

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-ch
		atexit.Exit(0)
	}()

	http.HandleFunc("/topic/create", createTopicHandler)
	//http.HandleFunc("/topics", GetTopicsHandler)
	http.HandleFunc("/topic", getTopicHandler)
	http.HandleFunc("/topic/comment", addCommentHandler)
	//http.HandleFunc("/topic/comments", GetCommentsHandler)
	http.HandleFunc("/topic/comments/range", getCommentsByIDRangeHandler)
	http.HandleFunc("/topic/comments/latest", getLatestCommentIDHandler)

	logger.Info("Starting server", "address", *commentServeAddr)
	if err := http.ListenAndServe(*commentServeAddr, nil); err != nil {
		logger.Error("discuss system is down", "error", err)
	}
}
