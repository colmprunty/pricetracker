# PriceTracker

A Go app that tracks grocery prices over time. CSV files are processed into a JSON store and displayed as a colour-coded HTML table showing price changes week to week.

## Requirements

- [Go 1.24+](https://go.dev/dl/)
- [templ](https://github.com/a-h/templ) (for regenerating the HTML template)

## Getting Started

```bash
git clone <repo-url>
cd PriceTracker
go run . process   # process CSVs into data/prices.json
go run .           # start the server
```

The server starts on port `8080` by default.

## Adding Price Data

Drop `.csv` files into `data/` using the naming convention:

```
YYYY-MM-DD-storename.csv
```

Each file must have `item` (or `name`) and `price` columns. `weight` (kg) and `per_100g` ($/100g) are optional:

```csv
item,price,weight,per_100g
Broccoli,4.49,,
Pepper Bell Yell Swt,2.42,0.220,1.10
Milk,7.49,,
```

All values are plain numbers — no `$` or unit suffixes. Weight is always in kg.

Then run `process` to merge new files into `data/prices.json`:

```bash
go run . process
```

Only new CSVs (not already in `prices.json`) are processed — existing data is preserved.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT`   | `8080`  | Port the server listens on |

## Endpoints

### `GET /`

HTML table showing all products across all shopping trips. Cells are colour-coded: red for price increases, green for price decreases vs the previous trip. Weight and price per 100g are shown where available.

### `GET /prices`

Raw pivot table as JSON. Each row is a product; each column is a shopping trip.

```bash
curl http://localhost:8080/prices
```

### `GET /health`

```bash
curl http://localhost:8080/health
# {"status":"ok"}
```

## Build

```bash
go build -o pricetracker .
./pricetracker process
./pricetracker
```
