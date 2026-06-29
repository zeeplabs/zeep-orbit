package orbit

type ClientConfig struct {
	BaseURL string
	App     string
	JWT     string
}

type ListResponse struct {
	Data   []map[string]any `json:"data"`
	Count  int              `json:"count"`
	Limit  int              `json:"limit"`
	Offset int              `json:"offset"`
}

type AuthLoginParams struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type AuthRegisterParams struct {
	Email    string `json:"email"`
	Password string `json:"password"`
	Name     string `json:"name,omitempty"`
}

type AuthResponse struct {
	Token        string `json:"token"`
	RefreshToken string `json:"refresh_token"`
	User         struct {
		ID    string `json:"id"`
		Email string `json:"email"`
		Name  string `json:"name,omitempty"`
	} `json:"user"`
}

type AuthUser struct {
	ID        string     `json:"id"`
	Email     string     `json:"email"`
	Name      string     `json:"name,omitempty"`
	Phone     string     `json:"phone,omitempty"`
	AvatarURL string     `json:"avatar_url,omitempty"`
}

type FileResponse struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Size      int64  `json:"size"`
	MimeType  string `json:"mime_type"`
	URL       string `json:"url"`
	CreatedAt string `json:"created_at"`
}

type SignedURLResponse struct {
	URL string `json:"url"`
}
