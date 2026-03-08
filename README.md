# PriceTracker

A lightweight Go API that reads CSV price files and serves a pivot table as JSON.

## Requirements

- [Go 1.20+](https://go.dev/dl/)

## Getting Started

```bash
git clone <repo-url>
cd PriceTracker
go run main.go
```

The server starts on port `8080` by default.

## Configuration

| Variable | Default | Description |
|----------|---------|-------------|
| `PORT`   | `8080`  | Port the server listens on |

```bash
PORT=9090 go run main.go
```

## Adding Price Data

Drop `.csv` files into the `data/` directory using the naming convention:

```
YYYY-MM-DD-storename.csv
```

Append `-1`, `-2`, etc. for multiple trips on the same day:

```
data/
├── 2024-01-08-tesco.csv
├── 2024-01-15-lidl.csv
└── 2024-01-15-tesco.csv
```

Each file must have `name` and `price` as its first two columns (case-insensitive):

```csv
name,price
Milk,3.99
Bread,2.50
```

Files are sorted alphabetically — the ISO date prefix guarantees chronological column order.

## API

### `GET /health`

```bash
curl http://localhost:8080/health
```

```json
{"status": "ok"}
```

---

### `GET /prices`

Returns a pivot table. Each row is a product; each column is a shopping trip (filename without `.csv`). Missing prices are `null`.

```bash
curl http://localhost:8080/prices
```

```json
{
  "columns": ["2024-01-08-tesco", "2024-01-15-lidl", "2024-01-15-tesco"],
  "rows": [
    { "name": "Bread",  "prices": ["2.50", "1.99", "2.50"] },
    { "name": "Butter", "prices": ["2.75", null,   null]   },
    { "name": "Cheese", "prices": [null,   "4.99",  "5.99"] },
    { "name": "Eggs",   "prices": ["3.25", null,   "3.40"] },
    { "name": "Milk",   "prices": ["3.99", "3.79",  "4.25"] },
    { "name": "Yoghurt","prices": [null,   "1.25",  null]   }
  ]
}
```

## Build

```bash
go build -o pricetracker .
./pricetracker
```
