package football

// Standing is one row of a group table.
type Standing struct {
	Position       int    `json:"position"`
	Team           string `json:"team"`
	Crest          string `json:"crest"`
	Played         int    `json:"played"`
	Won            int    `json:"won"`
	Draw           int    `json:"draw"`
	Lost           int    `json:"lost"`
	GoalDifference int    `json:"goalDifference"`
	Points         int    `json:"points"`
}

// Group is a named standings table (e.g. "Grupa A").
type Group struct {
	Name  string     `json:"name"`
	Table []Standing `json:"table"`
}
