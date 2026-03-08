package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

const dataDir = "./data"

type TableRow struct {
	Name   string    `json:"name"`
	Prices []*string `json:"prices"`
}

type TableResponse struct {
	Columns []string   `json:"columns"`
	Rows    []TableRow `json:"rows"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

// readCSVFile parses a CSV file with "name" and "price" header columns.
func readCSVFile(path string) (map[string]string, error) {
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
		return map[string]string{}, nil
	}

	header := records[0]
	if len(header) < 2 ||
		!strings.EqualFold(header[0], "name") ||
		!strings.EqualFold(header[1], "price") {
		return nil, fmt.Errorf("%s: expected columns 'name' and 'price', got %v", filepath.Base(path), header)
	}

	prices := make(map[string]string, len(records)-1)
	for _, row := range records[1:] {
		if len(row) < 2 {
			continue
		}
		prices[row[0]] = row[1]
	}
	return prices, nil
}

// buildTable reads all CSV files in dir and returns a pivot TableResponse.
func buildTable(dir string) (TableResponse, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return TableResponse{}, fmt.Errorf("read data directory: %w", err)
	}

	// Collect and sort CSV filenames alphabetically.
	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".csv") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	columns := make([]string, len(files))
	colData := make([]map[string]string, len(files))
	nameSet := make(map[string]struct{})

	for i, fname := range files {
		columns[i] = strings.TrimSuffix(fname, ".csv")
		data, err := readCSVFile(filepath.Join(dir, fname))
		if err != nil {
			return TableResponse{}, err
		}
		colData[i] = data
		for name := range data {
			nameSet[name] = struct{}{}
		}
	}

	// Collect and sort unique product names.
	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]TableRow, len(names))
	for i, name := range names {
		prices := make([]*string, len(columns))
		for j, data := range colData {
			if p, ok := data[name]; ok {
				v := p
				prices[j] = &v
			}
		}
		rows[i] = TableRow{Name: name, Prices: prices}
	}

	return TableResponse{Columns: columns, Rows: rows}, nil
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("encode response: %v", err)
	}
}

func pricesHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, ErrorResponse{Error: "method not allowed"})
		return
	}

	table, err := buildTable(dataDir)
	if err != nil {
		log.Printf("build table: %v", err)
		writeJSON(w, http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, table)
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
	mux.HandleFunc("/health", healthHandler)

	addr := ":" + port
	log.Printf("server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
