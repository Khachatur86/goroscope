# Export for Analysis

`goroscope export` writes timeline segments to stdout in CSV or JSON format for downstream analysis.

## Usage

```bash
goroscope export --format=csv capture.gtrace > segments.csv
goroscope export --format=json capture.gtrace > segments.json
```

Default format is CSV.

## CSV Columns

| Column        | Description                    |
|---------------|--------------------------------|
| goroutine_id  | Goroutine ID                   |
| state         | RUNNING, BLOCKED, WAITING, etc |
| start_ns      | Segment start (Unix nanoseconds) |
| end_ns        | Segment end (Unix nanoseconds) |
| reason        | Blocking reason (chan_recv, mutex_lock, etc) |
| resource_id   | Resource ID when blocked       |

## Python / pandas Example

```python
import pandas as pd

# Export from goroscope first: goroscope export --format=csv out.gtrace > segments.csv
df = pd.read_csv("segments.csv")
df["duration_ns"] = df["end_ns"] - df["start_ns"]
df["duration_ms"] = df["duration_ns"] / 1e6

# Total blocked time per goroutine
blocked = df[df["state"] == "BLOCKED"].groupby("goroutine_id")["duration_ns"].sum()
print(blocked.sort_values(ascending=False).head(10))
```

## JSON Format

```json
{
  "segments": [
    {
      "goroutine_id": 42,
      "state": "BLOCKED",
      "start_ns": 1739448000000000000,
      "end_ns": 1739448001000000000,
      "reason": "chan_recv",
      "resource_id": "chan:0xc000018230"
    }
  ]
}
```
