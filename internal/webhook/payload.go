package webhook

type CreateEvent struct {
	Ref        string     `json:"ref"`
	RefType    string     `json:"ref_type"`
	Repository Repository `json:"repository"`
}

type Repository struct {
	FullName string `json:"full_name"`
}
