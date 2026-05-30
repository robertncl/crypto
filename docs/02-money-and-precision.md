# Chapter 2 — Money & Precision

> **Learning objectives**
> - Understand why floating-point numbers are forbidden for money.
> - Learn the **fixed-point** technique exchanges use instead.
> - Read Nebula's `num.Dec` type and know when (and only when) a float is OK.

---

## 2.1 The bug that funds a thousand incident reports

Open any language REPL and try:

```js
0.1 + 0.2            // => 0.30000000000000004
```

This is not a bug in the language — it's how **IEEE-754 floating point** works.
Floats store numbers in binary, and `0.1` has no exact binary representation, the
same way `1/3` has no exact decimal representation. Tiny errors accumulate.

For graphics or physics, who cares. For **money**, it's catastrophic:

- Balances that should be exactly `0` end up as `-0.00000001`.
- Sums of millions of trades drift cent by cent until the books don't balance.
- "You have insufficient funds" when you clearly don't, because `x - x != 0`.

The rule, in every serious trading system:

> **Never represent money, prices, or quantities as floating-point numbers.**

Crypto makes this *worse* than traditional finance, because crypto amounts can be
both **very large** (billions of tokens) and **very precise** (Bitcoin divides
into 100,000,000 "satoshis"; some tokens have 18 decimals). You need exactness
across a huge range.

---

## 2.2 The fix: fixed-point integers

The standard solution is **fixed-point arithmetic**: pick a number of decimal
places, then store every value as an **integer** number of the smallest unit.

Bitcoin already works this way: the base unit is the **satoshi** = `0.00000001`
BTC (8 decimals). "1.5 BTC" is really `150,000,000` satoshis — an exact integer.

Nebula adopts 8 decimal places everywhere. The conversion factor is `10^8`:

```
real value   ×  100,000,000   =  stored integer
   1.5 BTC   ×  100,000,000   =  150,000,000
 95000.25 USDT × 100,000,000  =  9,500,025,000,000
```

Integers add and subtract **exactly**. `0.1 + 0.2` becomes `10,000,000 +
20,000,000 = 30,000,000`, which renders back as exactly `0.3`. Problem solved.

---

## 2.3 Nebula's `num.Dec` type

All of this lives in **`backend/internal/num/decimal.go`**. The type is a thin
wrapper around an `int64`:

```go
const Decimals = 8
const Scale int64 = 100_000_000   // 10^8

// Dec is a fixed-point decimal. The real value it represents is i / Scale.
type Dec struct {
    i int64
}
```

Wrapping the `int64` in a struct is deliberate: it makes it a **distinct type**,
so the compiler stops you from accidentally adding a raw integer (say, a count)
to a money value. You can only combine `Dec` with `Dec`.

### Addition and subtraction are trivial

```go
func (d Dec) Add(e Dec) Dec { return Dec{i: d.i + e.i} }
func (d Dec) Sub(e Dec) Dec { return Dec{i: d.i - e.i} }
```

### Multiplication and division need care (overflow!)

Here's a subtlety. To multiply two fixed-point numbers you must divide out one
factor of `Scale`:

```
(a × Scale) × (b × Scale) = a×b × Scale²      ← one Scale too many
```

So the math is `(d.i * e.i) / Scale`. But `d.i * e.i` can **overflow `int64`**
for realistic values (a price near 10^12 times a quantity near 10^12 is ~10^24,
far beyond int64's ~9.2×10^18). Nebula does the intermediate multiply in
arbitrary-precision `math/big` and only then narrows back to `int64`:

```go
func (d Dec) Mul(e Dec) Dec {
    a := big.NewInt(d.i)
    a.Mul(a, big.NewInt(e.i))
    a.Quo(a, bigScale) // divide out one Scale, truncating toward zero
    return Dec{i: a.Int64()}
}
```

This is the single most important line of defensive math in the codebase: it's
why computing a trade's value (`price.Mul(qty)`) can never silently overflow.

### Comparisons and helpers

`Dec` provides `Cmp`, `Lt`, `Lte`, `Gt`, `Gte`, `Eq`, `Sign`, `Neg`, `IsZero`,
plus `Min`/`Max`. The engine uses these constantly, e.g. "is the order's price a
positive multiple of the tick size?".

---

## 2.4 Crossing the wire: JSON as strings

Here's a trap that catches many teams. Even if your backend is careful, **JSON
numbers are IEEE-754 floats** in JavaScript. If the API sent
`"price": 95000.25` as a JSON *number*, the browser would parse it back into a
lossy float — re-introducing the very bug we eliminated.

Nebula serializes every `Dec` as a **JSON string**:

```go
func (d Dec) MarshalJSON() ([]byte, error) {
    return []byte(`"` + d.String() + `"`), nil
}
```

So the API emits `"price":"95000.25"`. The frontend treats prices and balances as
strings, formatting them for display and only converting to a `number` for things
that are inherently approximate anyway — like plotting a chart pixel. You'll see
this discipline in `web/src/utils/format.ts`.

---

## 2.5 When is a float allowed?

Almost never — but there are two legitimate uses, both **display-only**:

1. **Charts.** A candlestick is drawn on a pixel grid; sub-cent precision is
   invisible. `Dec.Float64()` exists for exactly this.
2. **The market-maker bot** (Chapter 11) generates *random* prices to simulate a
   market. Randomness doesn't need to be exact, so the bot computes in `float64`
   and converts to `Dec` at the boundary with `num.MustParse(ftoa(x))`.

The rule of thumb: **a float may decide what to draw or what random order to
place, but a float must never decide a balance.** Settlement math is 100%
`Dec`.

---

## Key takeaways

- Floating point is exact for fractions like 1/2 but not 1/10 — fatal for money.
- Exchanges use **fixed-point**: store integers of the smallest unit. Nebula uses
  **8 decimals** (`Scale = 10^8`), like Bitcoin's satoshi.
- `num.Dec` wraps an `int64`; `Mul`/`Div` route through `math/big` to avoid
  overflow.
- Money crosses the API as **JSON strings** to survive JavaScript.
- Floats are allowed only for **display** (charts) and **simulation** (the bot).

## Try it

- Read `backend/internal/num/decimal_test.go`. Notice the test
  `TestMulDivNoOverflow` deliberately uses large operands. Change `Mul` to do the
  naive `d.i * e.i / Scale` and watch the test fail.
- In a JS console: `JSON.parse('{"x": 9007199254740993}').x` — observe the value
  change. That number is just above `2^53`, the point where JSON numbers lose
  integer precision. Now you know why we send strings.

## Next

→ [Chapter 3 — The Custody Ledger](03-custody-ledger.md): now that we can
represent money exactly, how do we *move* it without ever losing a cent?
