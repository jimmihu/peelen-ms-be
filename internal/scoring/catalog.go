// Package scoring turns task fact counts into SLA scores and maturity levels.
// It has no database or HTTP dependencies.
package scoring

type Direction int

const (
	HigherBetter Direction = iota
	LowerBetter
)

type Unit string

const (
	UnitPercent Unit = "PERCENT"
	UnitMinutes Unit = "MINUTES"
	UnitScore   Unit = "SCORE"
)

type Indicator struct {
	Code      string    `json:"code"`
	Name      string    `json:"name"`
	Unit      Unit      `json:"unit"`
	Direction Direction `json:"-"`
	SLA       bool      `json:"sla"`
	Maturity  bool      `json:"maturity"`
}

// Catalog is the fixed list of indicators admins can pick from.
var Catalog = []Indicator{
	{Code: "COMPLETION_RATE", Name: "Tingkat penyelesaian", Unit: UnitPercent, Direction: HigherBetter, SLA: true, Maturity: true},
	{Code: "ON_TIME_RATE", Name: "Ketepatan waktu", Unit: UnitPercent, Direction: HigherBetter, SLA: true, Maturity: true},
	{Code: "AVG_LATE_MINUTES", Name: "Rata-rata keterlambatan", Unit: UnitMinutes, Direction: LowerBetter, SLA: true, Maturity: true},
	{Code: "LATE_UPLOAD_RATE", Name: "Upload susulan", Unit: UnitPercent, Direction: LowerBetter, SLA: true, Maturity: true},
	{Code: "EVIDENCE_COMPLETE_RATE", Name: "Kelengkapan foto", Unit: UnitPercent, Direction: HigherBetter, SLA: true, Maturity: true},
	{Code: "K3_FINDING_NOTES_RATE", Name: "Temuan K3 berpenjelasan", Unit: UnitPercent, Direction: HigherBetter, SLA: true, Maturity: true},
	{Code: "CONSISTENCY", Name: "Konsistensi", Unit: UnitScore, Direction: HigherBetter, SLA: false, Maturity: true},
}

func Lookup(code string) (Indicator, bool) {
	for _, indicator := range Catalog {
		if indicator.Code == code {
			return indicator, true
		}
	}
	return Indicator{}, false
}
