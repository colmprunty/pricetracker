# Grocery Receipt Parsing Instructions

## Goal
Extract grocery items from a FreshCo (or similar) receipt photo into a CSV file.

## Output Format
A CSV with the following columns:

| Column | Description |
|--------|-------------|
| `item` | Item name as printed on receipt |
| `price` | Full (undiscounted) per-unit price as a plain number. If a "YOU SAVED" line is present, add the saving back to the receipt price to get the original. If multiple units were bought (e.g. `2 @ 1/$1.29`), this is the per-unit price. |
| `discount_price` | Actual per-unit price paid after discount, as a plain number. Only populated if a "YOU SAVED" line is present. Otherwise blank. |
| `weight` | Weight in kg as a plain number, if shown on receipt. Otherwise blank. |
| `per_100g` | Cost per 100g as a plain number. Calculate from per-kg rate if needed (`rate / 10`). Otherwise blank. |

## Parsing Rules
1. "YOU SAVED $X.XX" lines apply to the item immediately above:
   - `discount_price` = price shown on receipt (per unit)
   - `price` = discount_price + savings amount
2. **Ignore** subtotal, tax, total, tender, and change lines
3. For weighted items, the receipt shows e.g. `0.460 kg @ $7.69 / kg` — extract weight as `0.460` and calculate per_100g as `7.69 / 10 = 0.769`
4. For fixed-price items with no weight info, leave `weight` and `per_100g` blank
5. For multi-unit purchases (e.g. `2 @ 1/$1.29`), record the per-unit price (total ÷ quantity) in both `price` and `discount_price` as appropriate
6. Strip units and currency symbols from all numeric columns — numbers only

## Example Rows
```
item,price,discount_price,weight,per_100g
Zucchini Squash,3.54,,0.460,0.77
Broccoli,4.49,,,
Beans French Green,4.99,3.99,,
Strawberries 1lb,5.99,2.44,,
Potato Sweet Orange,2.47,2.14,0.755,0.28
Avocados,1.29,,,
```

## Prompt to Use
> "Extract the grocery names and prices from this receipt image into a CSV.
> Columns: item, price, discount_price, weight, per_100g.
> price is always the full undiscounted per-unit price. If a 'YOU SAVED $X.XX' line follows an item, set discount_price to the receipt price and price to discount_price + savings. Otherwise leave discount_price blank.
> If multiple units were bought (e.g. `2 @ 1/$1.29`), use the per-unit price.
> For weighted items, extract weight in kg and calculate per_100g from the per-kg rate (divide by 10).
> All columns should be plain numbers with no units or currency symbols.
> Ignore subtotal, tax, and payment lines."

