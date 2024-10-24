package main

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"reflect"
	"syscall"
	"time"

	"github.com/gomarkdown/markdown"
)

type Entry struct {
	Title   string `xml:"title"`
	Updated string `xml:"updated"`
	Link    struct {
		Href string `xml:"href,attr"`
	} `xml:"link"`
}

type Feed struct {
	Entries []Entry `xml:"entry"`
}

type Config struct {
	FeedURLs      []string `json:"feed_urls"`
	MatrixServer  string   `json:"matrix_server"`
	MatrixRoomID  string   `json:"matrix_room_id"`
	MatrixToken   string   `json:"matrix_token"`
	CheckInterval int      `json:"check_interval"`
}

var config Config

func loadConfig(configPath string) error {
	file, err := os.Open(configPath)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	return decoder.Decode(&config)
}

func createDefaultConfig(configPath string) error {
	defaultConfig := Config{
		FeedURLs:      []string{"https://example.com/feed1", "https://example.com/feed2"},
		MatrixServer:  "https://matrix.org",
		MatrixRoomID:  "!yourroomid:matrix.org",
		MatrixToken:   "youraccesstoken",
		CheckInterval: 30,
	}
	configDir := filepath.Dir(configPath)
	if err := os.MkdirAll(configDir, 0755); err != nil {
		return err
	}
	configData, err := json.MarshalIndent(defaultConfig, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(configPath, configData, 0644)
}

func isDefaultConfig(config, defaultConfig Config) bool {
	return reflect.DeepEqual(config.FeedURLs, defaultConfig.FeedURLs) &&
		config.MatrixServer == defaultConfig.MatrixServer &&
		config.MatrixRoomID == defaultConfig.MatrixRoomID &&
		config.MatrixToken == defaultConfig.MatrixToken &&
		config.CheckInterval == defaultConfig.CheckInterval
}

func sendMatrixMessage(server, roomID, token, message string) error {
	htmlMessage := markdown.ToHTML([]byte(message), nil, nil)

	url := fmt.Sprintf("%s/_matrix/client/r0/rooms/%s/send/m.room.message?access_token=%s", server, roomID, token)
	payload := map[string]interface{}{
		"msgtype":        "m.text",
		"body":           message,
		"format":         "org.matrix.custom.html",
		"formatted_body": string(htmlMessage),
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", url, bytes.NewBuffer(payloadBytes))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("failed to send message: %s", resp.Status)
	}
	return nil
}

func fetchFeed(url string) (*Feed, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var feed Feed
	if err := xml.NewDecoder(resp.Body).Decode(&feed); err != nil {
		return nil, err
	}

	return &feed, nil
}

func main() {
	configPath := "/etc/matrix-rss/config.json"
	if err := loadConfig(configPath); err != nil {
		fmt.Println("Config not found, creating default config...")
		if err := createDefaultConfig(configPath); err != nil {
			fmt.Println("Error creating default config:", err)
			os.Exit(1)
		}
		fmt.Println("Default config created at /etc/matrix-rss/config.json. Please edit the config file and restart the program.")
		os.Exit(1)
	}

	defaultConfig := Config{
		FeedURLs:      []string{"https://example.com/feed1", "https://example.com/feed2"},
		MatrixServer:  "https://matrix.org",
		MatrixRoomID:  "!yourroomid:matrix.org",
		MatrixToken:   "youraccesstoken",
		CheckInterval: 30,
	}

	if isDefaultConfig(config, defaultConfig) {
		fmt.Println("Default config values detected. Please edit the config file at /etc/matrix-rss/config.json and restart the program.")
		os.Exit(1)
	}

	lastUpdates := make(map[string]string)

	signalChan := make(chan os.Signal, 1)
	signal.Notify(signalChan, syscall.SIGHUP)

	go func() {
		for range signalChan {
			fmt.Println("Reloading config...")
			if err := loadConfig(configPath); err != nil {
				fmt.Println("Error reloading config:", err)
			} else {
				fmt.Println("Config reloaded successfully")
			}
		}
	}()

	for {
		for _, feedURL := range config.FeedURLs {
			feed, err := fetchFeed(feedURL)
			if err != nil {
				fmt.Println("Error fetching feed:", err)
				continue
			}

			if len(feed.Entries) > 0 && feed.Entries[0].Updated != lastUpdates[feedURL] {
				lastUpdates[feedURL] = feed.Entries[0].Updated

				message := fmt.Sprintf("IT-News: [%s](%s)", feed.Entries[0].Title, feed.Entries[0].Link.Href)
				err = sendMatrixMessage(config.MatrixServer, config.MatrixRoomID, config.MatrixToken, message)
				if err != nil {
					fmt.Println("Error sending Matrix message:", err)
				} else {
					fmt.Println("Update message sent for feed:", feedURL)
				}
			}
		}

		time.Sleep(time.Duration(config.CheckInterval) * time.Minute)
	}
}
