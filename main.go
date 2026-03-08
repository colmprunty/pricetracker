package main

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
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
	URL string `json:"url"`
}

type CSVRow struct {
	Price string
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

var (
	reLDJSON       = regexp.MustCompile(`(?s)<script[^>]+type=["']application/ld\+json["'][^>]*>(.*?)</script>`)
	reWeightInName = regexp.MustCompile(`(\d+\.?\d*)\s*(kg|g)\b`)
	rePer100g      = regexp.MustCompile(`\$\s*(\d+\.?\d*)\s*per\s*100\s*g`)
)

// fetchPer100g fetches the product page and calculates price per 100g from
// JSON-LD structured data. Returns nil if it cannot be determined.
func fetchPer100g(productURL string) (*float64, error) {
	client := &http.Client{Timeout: 10 * time.Second}
	req, err := http.NewRequest(http.MethodGet, productURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}

	// Strategy 1: look for an explicit "$ X per 100g" string in the page.
	if m := rePer100g.FindSubmatch(body); m != nil {
		if rate, err := strconv.ParseFloat(string(m[1]), 64); err == nil && rate > 0 {
			return &rate, nil
		}
	}

	// Strategy 2: JSON-LD — price + weight in product name.
	for _, m := range reLDJSON.FindAllSubmatch(body, -1) {
		var obj map[string]any
		if err := json.Unmarshal(m[1], &obj); err != nil {
			continue
		}
		if typ, _ := obj["@type"].(string); typ != "Product" {
			continue
		}

		// Extract the listed price from offers.
		offers, _ := obj["offers"].(map[string]any)
		if offers == nil {
			continue
		}
		var price float64
		switch v := offers["price"].(type) {
		case float64:
			price = v
		case string:
			price, err = strconv.ParseFloat(v, 64)
			if err != nil || price <= 0 {
				continue
			}
		default:
			continue
		}

		// Extract weight from the product name, e.g. "Apples Ambrosia 1.36 kg".
		name, _ := obj["name"].(string)
		m := reWeightInName.FindStringSubmatch(strings.ToLower(name))
		if m == nil {
			continue
		}
		weightVal, err := strconv.ParseFloat(m[1], 64)
		if err != nil || weightVal <= 0 {
			continue
		}
		weightG := weightVal
		if m[2] == "kg" {
			weightG *= 1000
		}
		per100g := price / weightG * 100
		return &per100g, nil
	}
	return nil, nil
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

		// Fetch per_100g from each product's URL (once per product per CSV).
		per100g := make(map[string]*float64)
		for name := range csvData {
			url := meta[name].URL
			if url == "" {
				continue
			}
			rate, err := fetchPer100g(url)
			if err != nil {
				log.Printf("warn: fetch per_100g for %q: %v", name, err)
				continue
			}
			if rate != nil {
				log.Printf("  %s: $%.4f/100g", name, *rate)
			} else {
				log.Printf("  %s: per_100g not found on page", name)
			}
			per100g[name] = rate
		}

		colIdx := len(existing.Columns)
		existing.Columns = append(existing.Columns, col)

		// Extend existing rows with this week's price (or nil if not in CSV).
		seen := make(map[string]bool, len(existing.Rows))
		for i, row := range existing.Rows {
			seen[row.Name] = true
			existing.Rows[i].Prices = append(existing.Rows[i].Prices, makeCell(csvData[row.Name], per100g[row.Name]))
		}

		// Add new rows for products that weren't in any previous trip.
		var newRows []PriceRow
		for name, csvRow := range csvData {
			if seen[name] {
				continue
			}
			prices := make([]*PriceCell, colIdx+1) // nils for all prior columns
			prices[colIdx] = makeCell(csvRow, per100g[name])
			pm := meta[name]
			newRows = append(newRows, PriceRow{Name: name, URL: pm.URL, Prices: prices})
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

// makeCell builds a PriceCell from a CSVRow and an optional per-100g rate
// fetched from the product's website. Returns nil if the price is missing.
func makeCell(row CSVRow, per100g *float64) *PriceCell {
	if row.Price == "" {
		return nil
	}
	price, err := strconv.ParseFloat(row.Price, 64)
	if err != nil {
		return nil
	}
	cell := &PriceCell{Value: price}
	if per100g != nil && *per100g > 0 {
		wg := price / *per100g * 100
		cell.WeightG = &wg
		cell.Per100g = per100g
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

// readCSVFile parses a CSV file with "name" and "price" columns.
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

	nameIdx, priceIdx := -1, -1
	for i, h := range records[0] {
		switch strings.ToLower(strings.TrimSpace(h)) {
		case "name":
			nameIdx = i
		case "price":
			priceIdx = i
		}
	}
	if nameIdx == -1 || priceIdx == -1 {
		return nil, fmt.Errorf("%s: missing required columns 'name' and 'price'", filepath.Base(path))
	}

	rows := make(map[string]CSVRow, len(records)-1)
	for _, rec := range records[1:] {
		if len(rec) <= priceIdx {
			continue
		}
		rows[strings.TrimSpace(rec[nameIdx])] = CSVRow{Price: strings.TrimSpace(rec[priceIdx])}
	}
	return rows, nil
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
