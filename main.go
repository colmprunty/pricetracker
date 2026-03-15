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
	dataDir        = "./data"
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
	Value         float64  `json:"value"`
	WeightG       *float64 `json:"weight_g,omitempty"`
	Per100g       *float64 `json:"per_100g,omitempty"`
	DiscountPrice *float64 `json:"discount_price,omitempty"`
}

type PriceRow struct {
	Name   string       `json:"name"`
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
	Value      string     // "$4.49" or "" if missing
	Colour     CellColour
	Per100g    string // "$0.44" or ""
	WeightG    string // "223g" or ""
	Discounted string // "discounted to $x.xx" or ""
}

type UIRow struct {
	Name  string
	Cells []UICell
}

type UITable struct {
	Columns       []string
	ColumnColours []string
	Rows          []UIRow
}

type CSVRow struct {
	Price         string
	Per100g       string // optional, $/100g
	WeightKg      string // optional, kg
	DiscountPrice string // optional, discounted price
}

// processCSVs merges any new CSV files into prices.json.
// Existing data is preserved; only unprocessed CSVs are added.
func processCSVs() error {
	var existing PricesData
	if b, err := os.ReadFile(pricesDataFile); err == nil {
		if err := json.Unmarshal(b, &existing); err != nil {
			return fmt.Errorf("parse existing %s: %w", pricesDataFile, err)
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

		seen := make(map[string]bool, len(existing.Rows))
		for i, row := range existing.Rows {
			seen[row.Name] = true
			existing.Rows[i].Prices = append(existing.Rows[i].Prices, makeCell(csvData[row.Name]))
		}

		var newRows []PriceRow
		for name, csvRow := range csvData {
			if seen[name] {
				continue
			}
			prices := make([]*PriceCell, colIdx+1)
			prices[colIdx] = makeCell(csvRow)
			newRows = append(newRows, PriceRow{Name: name, Prices: prices})
		}
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

// makeCell builds a PriceCell from a CSVRow.
// per_100g and weight are optional; if both are absent the cell has no unit-price data.
// If only one is provided the other is back-calculated. Weight is in kg; stored as grams.
func makeCell(row CSVRow) *PriceCell {
	if row.Price == "" {
		return nil
	}
	price, err := strconv.ParseFloat(row.Price, 64)
	if err != nil {
		return nil
	}
	cell := &PriceCell{Value: price}

	per100g, hasRate := parseOptional(row.Per100g)
	weightKg, hasWeight := parseOptional(row.WeightKg)

	switch {
	case hasRate && hasWeight:
		cell.Per100g = &per100g
		wg := weightKg * 1000
		cell.WeightG = &wg
	case hasRate:
		cell.Per100g = &per100g
		wg := price / per100g * 100
		cell.WeightG = &wg
	case hasWeight:
		wg := weightKg * 1000
		cell.WeightG = &wg
		r := price / wg * 100
		cell.Per100g = &r
	}
	if dp, ok := parseOptional(row.DiscountPrice); ok {
		cell.DiscountPrice = &dp
	}
	return cell
}

func parseOptional(s string) (float64, bool) {
	if s == "" {
		return 0, false
	}
	v, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	return v, true
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
		var prevPer100g *float64
		var prevValue *float64
		for j, p := range row.Prices {
			if p == nil {
				cells[j] = UICell{Colour: CellNeutral}
				continue
			}
			colour := CellNeutral
			if p.Per100g != nil && prevPer100g != nil {
				if *p.Per100g > *prevPer100g {
					colour = CellIncreased
				} else if *p.Per100g < *prevPer100g {
					colour = CellDecreased
				}
			} else if p.Per100g == nil && prevValue != nil {
				if p.Value > *prevValue {
					colour = CellIncreased
				} else if p.Value < *prevValue {
					colour = CellDecreased
				}
			}
			if p.Per100g != nil {
				v := *p.Per100g
				prevPer100g = &v
			}
			v := p.Value
			prevValue = &v

			per100g, weightG := "", ""
			if p.Per100g != nil {
				per100g = fmt.Sprintf("$%.2f", *p.Per100g)
			}
			if p.WeightG != nil {
				weightG = fmt.Sprintf("%.0fg", *p.WeightG)
			}
			discounted := ""
			if p.DiscountPrice != nil {
				discounted = fmt.Sprintf("$%.2f", *p.DiscountPrice)
			}
			cells[j] = UICell{Value: fmt.Sprintf("$%.2f", p.Value), Colour: colour, Per100g: per100g, WeightG: weightG, Discounted: discounted}
		}
		rows[i] = UIRow{Name: row.Name, Cells: cells}
	}

	columns := make([]string, len(data.Columns))
	colours := make([]string, len(data.Columns))
	for i, c := range data.Columns {
		columns[i] = formatColumn(c)
		colours[i] = palette[i%len(palette)]
	}
	return UITable{Columns: columns, ColumnColours: colours, Rows: rows}, nil
}

// readCSVFile parses a CSV with required "name"/"item" and "price" columns,
// and optional "per_100g" and "weight" columns. All values are plain numbers.
// Weight is in kg. Prices and rates have no currency symbol.
func readCSVFile(path string) (map[string]CSVRow, error) {
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
		return map[string]CSVRow{}, nil
	}

	nameIdx, priceIdx, per100gIdx, weightIdx, discountIdx := -1, -1, -1, -1, -1
	for i, h := range records[0] {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "name", "item":
			nameIdx = i
		case "price":
			priceIdx = i
		case "per_100g":
			per100gIdx = i
		case "weight":
			weightIdx = i
		case "discount_price":
			discountIdx = i
		}
	}
	if nameIdx == -1 || priceIdx == -1 {
		return nil, fmt.Errorf("%s: missing required columns 'name'/'item' and 'price'", filepath.Base(path))
	}

	rows := make(map[string]CSVRow, len(records)-1)
	for _, rec := range records[1:] {
		if len(rec) <= priceIdx {
			continue
		}
		row := CSVRow{Price: strings.TrimSpace(rec[priceIdx])}
		if per100gIdx >= 0 && per100gIdx < len(rec) {
			row.Per100g = strings.TrimSpace(rec[per100gIdx])
		}
		if weightIdx >= 0 && weightIdx < len(rec) {
			row.WeightKg = strings.TrimSpace(rec[weightIdx])
		}
		if discountIdx >= 0 && discountIdx < len(rec) {
			row.DiscountPrice = strings.TrimSpace(rec[discountIdx])
		}
		rows[strings.TrimSpace(rec[nameIdx])] = row
	}
	return rows, nil
}

// buildTable reads all CSV files and returns a pivot TableResponse for /prices.
func buildTable(dir string) (TableResponse, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return TableResponse{}, fmt.Errorf("read data directory: %w", err)
	}

	var files []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(strings.ToLower(e.Name()), ".csv") {
			files = append(files, e.Name())
		}
	}
	sort.Strings(files)

	columns := make([]string, len(files))
	colData := make([]map[string]CSVRow, len(files))
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

	names := make([]string, 0, len(nameSet))
	for name := range nameSet {
		names = append(names, name)
	}
	sort.Strings(names)

	rows := make([]TableRow, len(names))
	for i, name := range names {
		prices := make([]*string, len(columns))
		for j, data := range colData {
			if row, ok := data[name]; ok {
				v := row.Price
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
