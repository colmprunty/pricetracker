package main

import (
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const dataDir = "./data"

type Item struct {
	Name   string `json:"name"`
	Price  string `json:"price"`
	Source string `json:"source,omitempty"`
}

type PricesResponse struct {
	Items []Item `json:"items"`
	Count int    `json:"count"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// readCSVFile parses a CSV file with "name" and "price" header columns.
func readCSVFile(path, source string) ([]Item, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	r := csv.NewReader(f)
	records, err := r.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("parse %s: %w", filepath.Base(path), err)
	}
	if len(records) == 0 {
		return nil, nil
	}

	header := records[0]
	if len(header) < 2 ||
		!strings.EqualFold(header[0], "name") ||
		!strings.EqualFold(header[1], "price") {
		return nil, fmt.Errorf("%s: expected columns 'name' and 'price', got %v", filepath.Base(path), header)
	}

	items := make([]Item, 0, len(records)-1)
	for _, row := range records[1:] {
		if len(row) < 2 {
			continue
		}
		items = append(items, Item{Name: row[0], Price: row[1], Source: source})
	}
	return items, nil
}

// readAll reads every CSV file in dir.
func readAll(dir string) ([]Item, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, fmt.Errorf("read data directory: %w", err)
	}

	var all []Item
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".csv") {
			continue
		}
		source := strings.TrimSuffix(e.Name(), ".csv")
		items, err := readCSVFile(filepath.Join(dir, e.Name()), source)
		if err != nil {
			return nil, err
		}
		all = append(all, items...)
	}
	return all, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

// pricesHandler serves GET /prices and GET /prices/{source}.
func pricesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	// Extract optional source name from path: /prices/{source}
	source := strings.Trim(strings.TrimPrefix(r.URL.Path, "/prices"), "/")

	var (
		items []Item
		err   error
	)

	if source != "" {
		items, err = readCSVFile(filepath.Join(dataDir, source+".csv"), source)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				writeJSON(w, http.StatusNotFound, ErrorResponse{Error: fmt.Sprintf("source '%s' not found", source)})
			} else {
				log.Printf("read csv: %v", err)
				writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			}
			return
		}
	} else {
		items, err = readAll(dataDir)
		if err != nil {
			log.Printf("read all csvs: %v", err)
			writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
			return
		}
	}

	if items == nil {
		items = []Item{}
	}
	writeJSON(w, http.StatusOK, PricesResponse{Items: items, Count: len(items)})
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func main() {
	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/prices", pricesHandler)
	mux.HandleFunc("/prices/", pricesHandler)
	mux.HandleFunc("/health", healthHandler)

	addr := ":" + port
	log.Printf("server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
