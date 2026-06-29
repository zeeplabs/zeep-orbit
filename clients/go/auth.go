package orbit

type AuthClient struct {
	client *Client
}

func (a *AuthClient) path(p string) string {
	return "auth/" + p
}

func (a *AuthClient) Login(params AuthLoginParams) (*AuthResponse, error) {
	var resp AuthResponse
	err := a.client.request("POST", a.path("login"), params, &resp)
	return &resp, err
}

func (a *AuthClient) Register(params AuthRegisterParams) (*AuthResponse, error) {
	var resp AuthResponse
	err := a.client.request("POST", a.path("register"), params, &resp)
	return &resp, err
}

func (a *AuthClient) Me() (*AuthUser, error) {
	var user AuthUser
	err := a.client.request("GET", a.path("me"), nil, &user)
	return &user, err
}

func (a *AuthClient) UpdateMe(data map[string]any) (*AuthUser, error) {
	var user AuthUser
	err := a.client.request("PUT", a.path("me"), data, &user)
	return &user, err
}

func (a *AuthClient) Logout() error {
	return a.client.request("POST", a.path("logout"), nil, nil)
}
