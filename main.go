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
	"strconv"
	"strings"
	"time"
)

const (
	dataDir       = "./data"
	pricesDataFile = "./data/prices.json"
)

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

// PriceCell is one entry in the processed prices.json file.
type PriceCell struct {
	Value   float64  `json:"value"`
	WeightG *float64 `json:"weight_g,omitempty"`
	Per100g *float64 `json:"per_100g,omitempty"`
}

type PriceRow struct {
	Name   string       `json:"name"`
	URL    string       `json:"url,omitempty"`
	Prices []*PriceCell `json:"prices"` // nil element = item not in that trip
}

type PricesData struct {
	GeneratedAt time.Time  `json:"generated_at"`
	Columns     []string   `json:"columns"`
	Rows        []PriceRow `json:"rows"`
}

type CellColour int

const (
	CellNeutral   CellColour = iota // first column, or no prior price
	CellIncreased                   // price went up   → red
	CellDecreased                   // price went down → green
)

type UICell struct {
	Value   string     // "$4.49" or "" if missing
	Colour  CellColour
	Per100g string     // "$0.44" or "" if not calculable
	WeightG string     // "223g" back-calculated from price/rate, or ""
}

type UIRow struct {
	Name  string
	URL   string // product page, may be empty
	Cells []UICell
}

type UITable struct {
	Columns       []string
	ColumnColours []string // CSS background color for each column header
	Rows          []UIRow
}

type ProductMeta struct {
	URL     string   `json:"url"`
	Per100g *float64 `json:"per_100g,omitempty"`
}

const productsFile = "./products.json"

func loadProductMeta() (map[string]ProductMeta, error) {
	data, err := os.ReadFile(productsFile)
	if err != nil {
		if os.IsNotExist(err) {
			return map[string]ProductMeta{}, nil
		}
		return nil, err
	}
	var m map[string]ProductMeta
	if err := json.Unmarshal(data, &m); err != nil {
		return nil, fmt.Errorf("parse %s: %w", productsFile, err)
	}
	return m, nil
}

// processCSVs merges any new CSV files into prices.json.
// Existing data is preserved; only unprocessed CSVs are added.
func processCSVs() error {
	// Load existing data (empty on first run).
	var existing PricesData
	if b, err := os.ReadFile(pricesDataFile); err == nil {
		if err := json.Unmarshal(b, &existing); err != nil {
			return fmt.Errorf("parse existing %s: %w", pricesDataFile, err)
		}
	}

	meta, err := loadProductMeta()
	if err != nil {
		return err
	}

	// Update URLs on existing rows from products.json.
	for i, row := range existing.Rows {
		if pm, ok := meta[row.Name]; ok && pm.URL != "" {
			existing.Rows[i].URL = pm.URL
		}
	}

	// Find CSVs not yet in prices.json.
	existingCols := make(map[string]bool, len(existing.Columns))
	for _, c := range existing.Columns {
		existingCols[c] = true
	}

	entries, err := os.ReadDir(dataDir)
	if err != nil {
		return fmt.Errorf("read data directory: %w", err)
	}
	var newFiles []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".csv") {
			col := strings.TrimSuffix(e.Name(), ".csv")
			if !existingCols[col] {
				newFiles = append(newFiles, e.Name())
			}
		}
	}
	sort.Strings(newFiles)

	if len(newFiles) == 0 {
		log.Println("nothing new to process")
		return nil
	}

	for _, fname := range newFiles {
		col := strings.TrimSuffix(fname, ".csv")
		csvData, err := readCSVFile(filepath.Join(dataDir, fname))
		if err != nil {
			return err
		}

		colIdx := len(existing.Columns)
		existing.Columns = append(existing.Columns, col)

		// Extend existing rows with this week's price (or nil if not in CSV).
		seen := make(map[string]bool, len(existing.Rows))
		for i, row := range existing.Rows {
			seen[row.Name] = true
			existing.Rows[i].Prices = append(existing.Rows[i].Prices, makeCell(csvData[row.Name], meta[row.Name]))
		}

		// Add new rows for products that weren't in any previous trip.
		var newRows []PriceRow
		for name, priceStr := range csvData {
			if seen[name] {
				continue
			}
			prices := make([]*PriceCell, colIdx+1) // nils for all prior columns
			prices[colIdx] = makeCell(priceStr, meta[name])
			pm := meta[name]
			newRows = append(newRows, PriceRow{Name: name, URL: pm.URL, Prices: prices})
		}
		// Sort new rows so the merge result stays alphabetical.
		sort.Slice(newRows, func(i, j int) bool { return newRows[i].Name < newRows[j].Name })
		existing.Rows = append(existing.Rows, newRows...)

		log.Printf("processed %s", fname)
	}

	sort.Slice(existing.Rows, func(i, j int) bool { return existing.Rows[i].Name < existing.Rows[j].Name })
	existing.GeneratedAt = time.Now().UTC()

	b, err := json.MarshalIndent(existing, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(pricesDataFile, b, 0644)
}

// makeCell builds a PriceCell from a raw price string, enriched with product meta.
// Returns nil if the price string is empty or unparseable.
func makeCell(priceStr string, pm ProductMeta) *PriceCell {
	if priceStr == "" {
		return nil
	}
	f, err := strconv.ParseFloat(priceStr, 64)
	if err != nil {
		return nil
	}
	cell := &PriceCell{Value: f}
	if pm.Per100g != nil && *pm.Per100g > 0 {
		wg := f / *pm.Per100g * 100
		cell.WeightG = &wg
		cell.Per100g = pm.Per100g
	}
	return cell
}

func loadPricesData() (PricesData, error) {
	b, err := os.ReadFile(pricesDataFile)
	if err != nil {
		return PricesData{}, fmt.Errorf("prices.json not found — run './pricetracker process' first: %w", err)
	}
	var data PricesData
	if err := json.Unmarshal(b, &data); err != nil {
		return PricesData{}, fmt.Errorf("parse %s: %w", pricesDataFile, err)
	}
	return data, nil
}

// formatColumn turns "2026-03-01-freshco" into "Mar 1, 2026 – Freshco".
func formatColumn(col string) string {
	// Expected format: YYYY-MM-DD-<store>
	parts := strings.SplitN(col, "-", 4)
	if len(parts) != 4 {
		return col
	}
	t, err := time.Parse("2006-01-02", parts[0]+"-"+parts[1]+"-"+parts[2])
	if err != nil {
		return col
	}
	store := strings.Title(strings.ReplaceAll(parts[3], "-", " "))
	return t.Format("Jan 2, 2006") + " – " + store
}

func buildUITable() (UITable, error) {
	data, err := loadPricesData()
	if err != nil {
		return UITable{}, err
	}

	palette := []string{"#dbeafe", "#fef9c3", "#dcfce7", "#fce7f3", "#ede9fe"}

	rows := make([]UIRow, len(data.Rows))
	for i, row := range data.Rows {
		cells := make([]UICell, len(row.Prices))
		var prevFloat *float64
		for j, p := range row.Prices {
			if p == nil {
				cells[j] = UICell{Colour: CellNeutral}
				continue
			}
			colour := CellNeutral
			if prevFloat != nil {
				if p.Value > *prevFloat {
					colour = CellIncreased
				} else if p.Value < *prevFloat {
					colour = CellDecreased
				}
			}
			f := p.Value
			prevFloat = &f

			per100g := ""
			weightG := ""
			if p.Per100g != nil {
				per100g = fmt.Sprintf("$%.2f", *p.Per100g)
			}
			if p.WeightG != nil {
				weightG = fmt.Sprintf("%.0fg", *p.WeightG)
			}
			cells[j] = UICell{Value: fmt.Sprintf("$%.2f", p.Value), Colour: colour, Per100g: per100g, WeightG: weightG}
		}
		rows[i] = UIRow{Name: row.Name, URL: row.URL, Cells: cells}
	}

	columns := make([]string, len(data.Columns))
	colours := make([]string, len(data.Columns))
	for i, c := range data.Columns {
		columns[i] = formatColumn(c)
		colours[i] = palette[i%len(palette)]
	}
	return UITable{Columns: columns, ColumnColours: colours, Rows: rows}, nil
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

func indexHandler(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}

	table, err := buildUITable()
	if err != nil {
		log.Printf("build UI table: %v", err)
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if err := tablePage(table).Render(r.Context(), w); err != nil {
		log.Printf("render template: %v", err)
	}
}

func healthHandler(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func main() {
	if len(os.Args) > 1 && os.Args[1] == "process" {
		if err := processCSVs(); err != nil {
			log.Fatalf("process: %v", err)
		}
		log.Printf("wrote %s", pricesDataFile)
		return
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	mux := http.NewServeMux()
	mux.HandleFunc("/", indexHandler)
	mux.HandleFunc("/prices", pricesHandler)
	mux.HandleFunc("/health", healthHandler)

	addr := ":" + port
	log.Printf("server listening on %s", addr)
	log.Fatal(http.ListenAndServe(addr, mux))
}
