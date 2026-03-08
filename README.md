# PriceTracker

A lightweight Go API that reads CSV price files and serves their contents as JSON.

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

Drop any `.csv` file into the `data/` directory. Each file must have `name` and `price` as its first two columns (case-insensitive).

```
data/
├── electronics.csv
└── groceries.csv
```

Example file:

```csv
name,price
Laptop,999.99
Wireless Mouse,29.99
```

The filename (without `.csv`) becomes the `source` field in the API response.

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

Returns all items from all CSV files.

```bash
curl http://localhost:8080/prices
```

```json
{
  "items": [
    {"name": "Laptop", "price": "999.99", "source": "electronics"},
    {"name": "Sourdough Bread", "price": "4.50", "source": "groceries"}
  ],
  "count": 2
}
```

---

### `GET /prices/{source}`

Returns items from a single CSV file, identified by its filename without the extension.

```bash
curl http://localhost:8080/prices/electronics
```

```json
{
  "items": [
    {"name": "Laptop", "price": "999.99", "source": "electronics"},
    {"name": "Wireless Mouse", "price": "29.99", "source": "electronics"}
  ],
  "count": 2
}
```

Returns `404` if no matching CSV file exists.

## Build

```bash
go build -o pricetracker .
./pricetracker
```
