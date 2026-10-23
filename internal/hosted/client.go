package hosted

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
)

type Client struct {
	endpoint   string
	httpClient *http.Client
}

func NewClient(endpoint string, httpClient *http.Client) *Client {
	// Custom authentication headers must never follow a redirect to another host.
	client := *httpClient
	client.CheckRedirect = func(*http.Request, []*http.Request) error {
		return http.ErrUseLastResponse
	}
	return &Client{
		endpoint:   endpoint,
		httpClient: &client,
	}
}

type ProfileRequest struct {
	Token           string
	UserID          string
	AppID           string
	FromSupportMenu string
}

type Profile struct {
	ACL  ACL  `json:"acl"`
	User User `json:"user"`
	App  App  `json:"app"`
}

type ACL struct {
	Name string          `json:"name"`
	List []ACLPermission `json:"list"`
}

type ACLPermission struct {
	Name        string   `json:"name"`
	Permissions []string `json:"permissions"`
}

type User struct {
	ID          string `json:"id"`
	Email       string `json:"email"`
	FirstName   string `json:"firstName"`
	LastName    string `json:"lastName"`
	NupRole     string `json:"nupRole"`
	PhoneNumber string `json:"phoneNumber"`
}

type App struct {
	ID      string     `json:"id"`
	Profile AppProfile `json:"profile"`
}

type AppProfile struct {
	Representative string `json:"representative"`
	Domain         string `json:"domain"`
	Limits         Limits `json:"limits"`
}

type Limits struct {
	Extens     int `json:"extens"`
	Queues     int `json:"queues"`
	Trunks     int `json:"trunks"`
	VoiceMails int `json:"voiceMails"`
	RestAPI    int `json:"restApi"`
	EventAPI   int `json:"eventApi"`
}

type graphQLRequest struct {
	Query     string         `json:"query"`
	Variables map[string]any `json:"variables"`
}

type graphQLResponse struct {
	Data struct {
		NupProfileAppInfo Profile `json:"nupProfileAppInfo"`
	} `json:"data"`

	Errors []graphQLError `json:"errors"`
}

type graphQLError struct {
	Message string `json:"message"`
}

func (c *Client) NupProfileAppInfo(
	ctx context.Context,
	input ProfileRequest,
) (Profile, error) {
	const query = `
		query nupProfileAppInfo($appId: String, $fromSupportMenu: String) {
			nupProfileAppInfo(
				appId: $appId,
				fromSupportMenu: $fromSupportMenu
			) {
				acl {
					list {
						name
						permissions
					}
					name
				}
				user {
					id
					email
					firstName
					lastName
					nupRole
					phoneNumber
				}
				app {
					id
					profile {
						representative
						domain
						limits {
							extens
							queues
							trunks
							voiceMails
							restApi
							eventApi
						}
					}
				}
			}
		}
	`

	payload := graphQLRequest{
		Query: query,
		Variables: map[string]any{
			"appId":           input.AppID,
			"fromSupportMenu": input.FromSupportMenu,
		},
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return Profile{}, fmt.Errorf("marshal graphql request: %w", err)
	}

	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		c.endpoint,
		bytes.NewReader(body),
	)
	if err != nil {
		return Profile{}, fmt.Errorf("create graphql request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-APIKEY", input.Token)
	req.Header.Set("APP-ID", input.AppID)
	req.Header.Set("USER-ID", input.UserID)
	req.Header.Set("FROM-SUPPORT-MENU", input.FromSupportMenu)

	res, err := c.httpClient.Do(req)
	if err != nil {
		return Profile{}, fmt.Errorf("call hosted graphql: %w", err)
	}
	defer res.Body.Close()

	if res.StatusCode < 200 || res.StatusCode >= 300 {
		return Profile{}, fmt.Errorf(
			"hosted graphql returned status %d",
			res.StatusCode,
		)
	}

	var response graphQLResponse

	err = json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(&response)
	if err != nil {
		return Profile{}, fmt.Errorf("decode graphql response: %w", err)
	}

	if len(response.Errors) > 0 {
		return Profile{}, fmt.Errorf(
			"hosted graphql: %s",
			response.Errors[0].Message,
		)
	}

	return response.Data.NupProfileAppInfo, nil
}
