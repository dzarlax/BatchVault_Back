package services

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	apnsDevelopmentEndpoint = "https://api.sandbox.push.apple.com"
	apnsProductionEndpoint  = "https://api.push.apple.com"
	apnsTokenLifetime       = 50 * time.Minute
)

var ErrAPNSDelivery = errors.New("APNs delivery request failed")

// APNSPayload contains the safe, minimal notification data sent to the client.
type APNSPayload struct {
	APS         APNSAlertPayload `json:"aps"`
	Type        string           `json:"type"`
	OrderID     uint             `json:"order_id"`
	WorkspaceID uint             `json:"workspace_id"`
}

// APNSAlertPayload contains generic notification text only.
type APNSAlertPayload struct {
	Alert APNSAlert `json:"alert"`
	Sound string    `json:"sound"`
}

// APNSAlert is the visible generic notification summary.
type APNSAlert struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

// APNSDelivery is the boundary used to send a notification to one APNs device.
type APNSDelivery interface {
	Enabled() bool
	Environment() string
	Send(context.Context, string, APNSPayload) error
}

// APNSError is a token-free APNs response error.
type APNSError struct {
	StatusCode int
	Reason     string
}

func (err *APNSError) Error() string {
	if err.Reason == "" {
		return "APNs rejected notification"
	}
	return "APNs rejected notification: " + err.Reason
}

// IsInvalidDeviceTokenError reports whether APNs rejected a stale device token.
func IsInvalidDeviceTokenError(err error) bool {
	var apnsErr *APNSError
	if !errors.As(err, &apnsErr) {
		return false
	}
	return apnsErr.Reason == "BadDeviceToken" || apnsErr.Reason == "Unregistered"
}

type apnsConfig struct {
	keyID       string
	teamID      string
	topic       string
	environment string
	endpoint    string
}

// APNSClient delivers alerts to APNs using ES256 token authentication.
type APNSClient struct {
	config     apnsConfig
	privateKey *ecdsa.PrivateKey
	httpClient *http.Client
	now        func() time.Time

	mu                    sync.Mutex
	authorization         string
	authorizationIssuedAt time.Time
}

// NewAPNSClientFromEnvironment creates a disabled client when APNs configuration is absent or invalid.
func NewAPNSClientFromEnvironment() *APNSClient {
	encodedKey := strings.TrimSpace(os.Getenv("APNS_AUTH_KEY_BASE64"))
	config := apnsConfig{
		keyID:       strings.TrimSpace(os.Getenv("APNS_KEY_ID")),
		teamID:      strings.TrimSpace(os.Getenv("APNS_TEAM_ID")),
		topic:       strings.TrimSpace(os.Getenv("APNS_TOPIC")),
		environment: strings.TrimSpace(os.Getenv("APNS_ENVIRONMENT")),
	}

	if encodedKey == "" && config.keyID == "" && config.teamID == "" && config.topic == "" && config.environment == "" {
		return &APNSClient{}
	}
	if encodedKey == "" || config.keyID == "" || config.teamID == "" || config.topic == "" || config.environment == "" {
		return &APNSClient{}
	}

	switch config.environment {
	case "development":
		config.endpoint = apnsDevelopmentEndpoint
	case "production":
		config.endpoint = apnsProductionEndpoint
	default:
		return &APNSClient{}
	}

	privateKey, err := parseAPNSPrivateKey(encodedKey)
	if err != nil {
		return &APNSClient{}
	}

	return &APNSClient{
		config:     config,
		privateKey: privateKey,
		httpClient: &http.Client{Timeout: 10 * time.Second},
		now:        time.Now,
	}
}

func parseAPNSPrivateKey(encodedKey string) (*ecdsa.PrivateKey, error) {
	keyBytes, err := base64.StdEncoding.DecodeString(encodedKey)
	if err != nil {
		return nil, err
	}
	if pemBlock, _ := pem.Decode(keyBytes); pemBlock != nil {
		keyBytes = pemBlock.Bytes
	}
	privateKey, err := x509.ParsePKCS8PrivateKey(keyBytes)
	if err != nil {
		return nil, err
	}
	ecdsaPrivateKey, ok := privateKey.(*ecdsa.PrivateKey)
	if !ok || ecdsaPrivateKey.Curve != elliptic.P256() {
		return nil, errors.New("APNs authentication key must use P-256")
	}
	return ecdsaPrivateKey, nil
}

// Enabled reports whether the client has a complete, valid APNs configuration.
func (client *APNSClient) Enabled() bool {
	return client != nil && client.privateKey != nil && client.config.endpoint != "" && client.httpClient != nil
}

// Environment returns the APNs environment used by this client.
func (client *APNSClient) Environment() string {
	if client == nil {
		return ""
	}
	return client.config.environment
}

// Send delivers a single APNs alert. Returned errors never include a device token.
func (client *APNSClient) Send(ctx context.Context, deviceToken string, payload APNSPayload) error {
	if !client.Enabled() {
		return nil
	}

	body, err := json.Marshal(payload)
	if err != nil {
		return ErrAPNSDelivery
	}
	authorization, err := client.authToken()
	if err != nil {
		return ErrAPNSDelivery
	}

	endpoint := client.config.endpoint + "/3/device/" + url.PathEscape(deviceToken)
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return ErrAPNSDelivery
	}
	request.Header.Set("Authorization", "bearer "+authorization)
	request.Header.Set("Apns-Topic", client.config.topic)
	request.Header.Set("Apns-Push-Type", "alert")
	request.Header.Set("Content-Type", "application/json")

	response, err := client.httpClient.Do(request)
	if err != nil {
		return ErrAPNSDelivery
	}
	defer response.Body.Close()
	if response.StatusCode >= http.StatusOK && response.StatusCode < http.StatusMultipleChoices {
		return nil
	}

	var responseBody struct {
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(io.LimitReader(response.Body, 4096)).Decode(&responseBody)
	return &APNSError{StatusCode: response.StatusCode, Reason: responseBody.Reason}
}

func (client *APNSClient) authToken() (string, error) {
	client.mu.Lock()
	defer client.mu.Unlock()

	now := client.now().UTC()
	if client.authorization != "" && now.Sub(client.authorizationIssuedAt) < apnsTokenLifetime {
		return client.authorization, nil
	}

	header, err := json.Marshal(map[string]string{"alg": "ES256", "kid": client.config.keyID})
	if err != nil {
		return "", err
	}
	claims, err := json.Marshal(map[string]interface{}{"iss": client.config.teamID, "iat": now.Unix()})
	if err != nil {
		return "", err
	}
	unsigned := base64.RawURLEncoding.EncodeToString(header) + "." + base64.RawURLEncoding.EncodeToString(claims)
	digest := sha256.Sum256([]byte(unsigned))
	r, s, err := ecdsa.Sign(rand.Reader, client.privateKey, digest[:])
	if err != nil {
		return "", err
	}

	signature := make([]byte, 64)
	r.FillBytes(signature[:32])
	s.FillBytes(signature[32:])
	client.authorization = unsigned + "." + base64.RawURLEncoding.EncodeToString(signature)
	client.authorizationIssuedAt = now
	return client.authorization, nil
}
