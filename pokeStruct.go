package pokedict

type PokemonSkill struct {
	Id       int64
	Kind     string
	Type     string
	Name     string
	Cname    string
	Damage   float64
	Cooldown float64
	Energy   float64
	Dps      float64
}

type Pokemon struct {
	Id             int64 "json:,string"
	Classification string
	Name           string
	Cname          string
	MaxCP          int64
	TypeI          string `json:"Type I"`
	TypeII         string `json:"Type II,omitempty"`
	Weaknesses     []string
	FastMoves      []string `json:"Fast Attack(s)"`
	ChargedMoves   []string `json:"Special Attack(s)"`
}
