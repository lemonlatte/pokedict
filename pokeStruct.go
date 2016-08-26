package pokedict

type PokemonSkill struct {
	Id       int
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
	Id             int "json:,string"
	Classification string
	Name           string
	Cname          string
	MaxCP          int
	TypeI          string `json:"Type I"`
	TypeII         string `json:"Type II,omitempty"`
	Weaknesses     []string
	FastMoves      []string `json:"Fast Attack(s)"`
	ChargedMoves   []string `json:"Special Attack(s)"`
}
