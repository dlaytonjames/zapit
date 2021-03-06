package main

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"sync"
	"time"

	"github.com/ihcsim/zapit"
	"github.com/ihcsim/zapit/internal/db"
)

const (
	endpoint         = "/urlinfo/1/"
	defaultPort      = "8080"
	defaultDBService = "db"
	defaultDBPort    = "6379"

	envHostname  = "HOSTNAME"
	envPort      = "PORT"
	envDBService = "DB_SERVICE"
	envDBPort    = "DB_PORT"

	contentType = "application/json; charset=utf-8"

	dbProtocol = "tcp"
	dbTimeout  = time.Second * 2
)

var (
	scanner  *zapit.Scanner
	database zapit.Database
	once     sync.Once
)

func main() {
	// connect to db
	dbURL := dbHost()
	log.Printf("Connecting to database at %s", dbURL)
	if err := initDB(dbURL); err != nil {
		log.Fatal("Can't connect to db: ", err)
	}

	// handle interrupt signal
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt)
	go catchInterrupt(quit, database)

	// init scanner
	initScanner(database)

	// register handler with DefaultServeMux
	http.HandleFunc(endpoint, handleURLInfo)

	// set up listener
	listenAddr := serverURL()
	log.Printf("Listening at %s\n", listenAddr)
	if err := http.ListenAndServe(listenAddr, nil); err != nil {
		log.Fatalf("Fail to start up server at %s. Cause: %s\n", listenAddr, err)
	}
}

func dbHost() string {
	service, exist := os.LookupEnv(envDBService)
	if !exist {
		service = defaultDBService
	}

	port, exist := os.LookupEnv(envDBPort)
	if !exist {
		port = defaultDBPort
	}

	return fmt.Sprintf("%s:%s", service, port)
}

func initDB(host string) error {
	var err error
	database, err = db.NewRedis(host, dbProtocol, dbTimeout)
	return err
}

func catchInterrupt(c <-chan os.Signal, db zapit.Database) {
	for {
		select {
		case <-c:
			log.Println("Shutting down server...")
			if err := db.Close(); err != nil {
				log.Println(err)
				os.Exit(1)
			}
			os.Exit(0)
		}
	}
}

func initScanner(db zapit.Database) {
	once.Do(func() {
		scanner = zapit.NewScanner(db)
	})
}

func handleURLInfo(w http.ResponseWriter, req *http.Request) {
	hostname, e := os.Hostname()
	if e != nil {
		responseBadRequest(w, e)
		return
	}
	log.Printf("[%s]: GET %s", hostname, req.URL)

	var (
		result *zapit.URLInfo
		err    error
	)

	raw := strings.TrimPrefix(req.URL.Path, endpoint)
	unescaped, err := url.PathUnescape(raw)
	if err != nil {
		responseBadRequest(w, err)
		return
	}

	result, err = scanner.IsSafe(unescaped)
	if err != nil {
		if zapit.IsMalformedURLError(err) {
			responseBadRequest(w, err)
			return
		}
		responseError(w, err)
		return
	}

	result.URL = url.QueryEscape(result.URL)
	content, err := json.Marshal(result)
	if err != nil {
		responseError(w, err)
		return
	}

	log.Printf("%s\n", content)
	responseOK(w, content)
}

func serverURL() string {
	hostname := os.Getenv(envHostname)
	port, exist := os.LookupEnv(envPort)
	if !exist {
		port = defaultPort
	}

	return fmt.Sprintf("%s:%s", hostname, port)
}

func responseBadRequest(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusBadRequest)
	w.Header().Set("Content-Type", contentType)

	content := fmt.Sprintf(`{"error": "%s"}`, err)
	w.Write([]byte(content))
}

func responseError(w http.ResponseWriter, err error) {
	w.WriteHeader(http.StatusInternalServerError)
	w.Header().Set("Content-Type", contentType)

	content := fmt.Sprintf(`{"error": "%s"}`, err)
	w.Write([]byte(content))
}

func responseOK(w http.ResponseWriter, b []byte) {
	w.WriteHeader(http.StatusOK)
	w.Header().Set("Content-Type", contentType)
	w.Write(b)
}
