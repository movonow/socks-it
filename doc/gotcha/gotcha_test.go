package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"log"
	"os"
	"testing"
)

func base64StreamInterface(data string) string {
	encodeBuffer := new(bytes.Buffer)
	encoder := base64.NewEncoder(base64.StdEncoding, encodeBuffer)
	if _, err := encoder.Write([]byte(data)); err != nil {
		log.Fatal(err)
	}

	decoder := base64.NewDecoder(base64.StdEncoding, encodeBuffer)
	decodeBuffer := new(bytes.Buffer)
	if _, err := io.Copy(decodeBuffer, decoder); err != nil {
		log.Fatal(err)
	}

	return decodeBuffer.String()
}

func Test_base64StreamInterface(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "3",
			data: "ABC",
			want: "ABC",
		},
		{
			name: "4",
			data: "ABCD",
			want: "ABCD",
		},
		{
			name: "5",
			data: "ABCDE",
			want: "ABCDE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := base64StreamInterface(tt.data); got != tt.want {
				t.Errorf("base64StreamInterface() = %v, want %v", got, tt.want)
			}
		})
	}
}

func base64ValueInterface(data string) string {
	encoded := base64.StdEncoding.EncodeToString([]byte(data))

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		log.Fatal(err)
	}

	return string(decoded)
}

func Test_base64ValueInterface(t *testing.T) {
	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "3",
			data: "ABC",
			want: "ABC",
		},
		{
			name: "4",
			data: "ABCD",
			want: "ABCD",
		},
		{
			name: "5",
			data: "ABCDE",
			want: "ABCDE",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := base64ValueInterface(tt.data); got != tt.want {
				t.Errorf("base64StreamInterface() = %v, want %v", got, tt.want)
			}
		})
	}
}

type jsonWriter struct {
	reader io.Reader
}

func (w *jsonWriter) Write(p []byte) (n int, err error) {
	if w.reader == nil {
		w.reader = bytes.NewReader(p)
	} else {
		w.reader = io.MultiReader(w.reader, bytes.NewReader(p))
	}
	return len(p), nil
}

func Test_jsonMarshalAndUnmarshalSequentially(t *testing.T) {
	type S struct {
		Name string `json:"name"`
		Team string `json:"team"`
	}

	player1Writer := &jsonWriter{}

	player1 := S{
		Name: "kobe",
		Team: "lakers",
	}
	if err := json.NewEncoder(player1Writer).Encode(&player1); err != nil {
		log.Fatal(err)
	}

	if _, err := io.Copy(os.Stdout, player1Writer.reader); err != nil {
		log.Fatal(err)
	}

	player2 := S{
		Name: "kevin",
		Team: "wolves",
	}
	_, err := json.Marshal(&player2)
	if err != nil {
		log.Fatal(err)
	}
}

func Test_jsonMarshalAndUnmarshalInterleaved(t *testing.T) {
	type S struct {
		Name string `json:"name"`
		Team string `json:"team"`
	}

	player1Writer := &jsonWriter{}

	player1 := S{
		Name: "kobe",
		Team: "lakers",
	}
	if err := json.NewEncoder(player1Writer).Encode(&player1); err != nil {
		log.Fatal(err)
	}

	if _, err := io.Copy(os.Stdout, player1Writer.reader); err != nil {
		log.Fatal(err)
	}

	player2 := S{
		Name: "kevin",
		Team: "wolves",
	}
	_, err := json.Marshal(&player2)
	if err != nil {
		log.Fatal(err)
	}

	//if _, err := io.Copy(os.Stdout, player1Writer.reader); err != nil {
	//	log.Fatal(err)
	//}
}
