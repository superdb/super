package nano

import (
	"encoding/json"
	"fmt"
	"math"
	"strconv"
	"time"
)

type Ts int64

const (
	MinTs Ts = 0
	MaxTs Ts = math.MaxInt64

	maxMillis = MaxTs / 1000000
)

type jsonTs struct {
	Sec int64 `json:"sec"`
	Ns  int64 `json:"ns"`
}

func access(m map[string]any, field string) (int64, bool) {
	if v, ok := m[field]; ok {
		f, ok := v.(float64)
		if ok {
			return int64(f), true
		}
	}
	return 0, false
}

func (t *Ts) UnmarshalJSON(in []byte) error {
	var v any
	if err := json.Unmarshal(in, &v); err != nil {
		return err
	}
	switch v := v.(type) {
	case string:
		i, err := strconv.ParseInt(v, 10, 64)
		if err != nil {
			return fmt.Errorf("invalid time format: %s", in)
		}
		*t = Ts(i)
		return nil
	case float64:
		*t = Ts(v)
		return nil
	case map[string]any:
		sec, ok := access(v, "sec")
		if ok {
			ns, ok := access(v, "ns")
			if !ok {
				ns = 0
			}
			*t = Ts(sec*1000000000 + ns)
			return nil
		}
		return fmt.Errorf("time object is not of the form {sec:x, ns:y}")
	}
	return fmt.Errorf("unsupported time format: %T", v)
}

// Split returns the seconds and nanoseconds since epoch of the timestamp.
func (t Ts) Split() (int64, int64) {
	sec := int64(t / 1000000000)
	ns := int64(t) - sec*1000000000
	return sec, ns
}

func (t Ts) MarshalJSON() ([]byte, error) {
	sec, ns := t.Split()
	v := jsonTs{sec, ns}
	return json.Marshal(&v)
}

func (t Ts) Time() time.Time {
	sec, ns := t.Split()
	return time.Unix(sec, ns).UTC()
}

func (t Ts) Trunc(bin Duration) Ts {
	return Ts(Duration(t).Trunc(bin))
}

func (t Ts) Midnight() Ts {
	return t.Trunc(Day)
}

func (t Ts) DayOf() Span {
	return Span{t.Midnight(), Day}
}

func (t Ts) String() string {
	return t.Time().Format(time.RFC3339)
}

func (t Ts) Pretty() string {
	return t.Time().Format("01/02/2006@15:04:05")
}

func (t Ts) Add(d Duration) Ts {
	return Ts(int64(t) + int64(d))
}

func (t Ts) Sub(d Duration) Ts {
	return Ts(int64(t) - int64(d))
}

// SubTs returns the duration t-u.
func (t Ts) SubTs(u Ts) Duration {
	return Duration(t - u)
}

// convert a golang time to a nano Ts
func TimeToTs(t time.Time) Ts {
	return Ts(t.UnixNano())
}

func Date(year int, month time.Month, day, hour, min, sec, nsec int) Ts {
	t := time.Date(year, month, day, hour, min, sec, nsec, time.UTC)
	return TimeToTs(t)
}

func Now() Ts {
	return TimeToTs(time.Now())
}

// ParseMillis parses an unsigned integer representing milliseconds since the
// Unix epoch.
func ParseMillis(s []byte) (Ts, error) {
	if len(s) == 0 {
		return 0, fmt.Errorf("invalid time format: %s", string(s))
	}
	var v int64
	for _, c := range s {
		d := c - '0'
		if d > 9 {
			return 0, fmt.Errorf("invalid time format: %s", string(s))
		}
		v = v*10 + int64(d)
	}
	if Ts(v) > maxMillis {
		return 0, fmt.Errorf("overflow in Millis time format: %s", string(s))
	}
	return Ts(v * 1_000_000), nil
}

// ParseRFC3339Nano parses a byte according to the time.RFC3339Nano
// format into a Ts, returning an error if parsing failed.
func ParseRFC3339Nano(s []byte) (Ts, error) {
	t, err := time.Parse(time.RFC3339Nano, string(s))
	if err != nil {
		return 0, err
	}
	return TimeToTs(t), nil
}

// Unix returns a Ts corresponding to the given Unix time, sec seconds
// and nsec nanoseconds since January 1, 1970 UTC.
func Unix(sec, ns int64) Ts {
	return Ts(sec*1000000000 + ns)
}
