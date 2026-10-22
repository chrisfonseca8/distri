package health

type Status struct {
	Healthy bool            `json:"healthy"`
	Checks  map[string]bool `json:"checks"`
}
