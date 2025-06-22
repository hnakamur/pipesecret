package internal

type HeartbeatRequest struct {
	SecretResponse *SecretResponse `json:",omitempty"`
}

type HeartbeatResponse struct {
	SecretRequest *SecretRequest `json:",omitempty"`
}

type SecretRequest struct {
	ID    string
	Items []SecretRequestItem
}

type SecretRequestItem struct {
	ItemName string
	Query    string
}

type SecretResponse struct {
	ID    string
	Items []SecretResponseItem
}

type SecretResponseItem struct {
	ItemName string
	Result   string
}
