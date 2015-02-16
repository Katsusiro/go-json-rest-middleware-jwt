package rest

import (
	"github.com/ant0ine/go-json-rest/rest"
	"github.com/dgrijalva/jwt-go"

	"errors"
	"log"
	"net/http"
	"strings"
	"time"
)

// JWTMiddleware provides a Json-Webtoken authentication implementation. On failure, a 401 HTTP response
// is returned. On success, the wrapped middleware is called, and the userId is made available as
// request.Env["REMOTE_USER"].(string)
type JWTMiddleware struct {
	// Realm name to display to the user. Required.
	Realm string

	// signing algorithm - possible values are HS256, HS384, HS512
	// Optional, default is HS256
	SigningAlgorithm string

	// Secret key used for signing. Required
	Key []byte

	// Duration that a jwt token is valid. Optional, default is one hour
	Timeout time.Duration

	// if max refresh period
	MaxRefresh time.Duration

	// Callback function that should perform the authentication of the user based on userId and
	// password. Must return true on success, false on failure. Required.
	Authenticator func(userId string, password string) bool

	// Callback function that should perform the authorization of the authenticated user. Called
	// only after an authentication success. Must return true on success, false on failure.
	// Optional, default to success.
	Authorizator func(userId string, request *rest.Request) bool
}

// MiddlewareFunc makes JWTMiddleware implement the Middleware interface.
func (mw *JWTMiddleware) MiddlewareFunc(handler rest.HandlerFunc) rest.HandlerFunc {

	if mw.Realm == "" {
		log.Fatal("Realm is required")
	}
	if mw.SigningAlgorithm == "" {
		mw.SigningAlgorithm = "HS256"
	}
	if mw.Key == nil {
		log.Fatal("Key required")
	}
	if mw.Timeout == 0 {
		mw.Timeout = time.Hour
	}
	if mw.Authenticator == nil {
		log.Fatal("Authenticator is required")
	}
	if mw.Authorizator == nil {
		mw.Authorizator = func(userId string, request *rest.Request) bool {
			return true
		}
	}

	return func(writer rest.ResponseWriter, request *rest.Request) { mw.middlewareImpl(writer, request, handler) }
}

func (mw *JWTMiddleware) middlewareImpl(writer rest.ResponseWriter, request *rest.Request, handler rest.HandlerFunc) {
	token, err := parseToken(request, mw.Key)

	if err != nil {
		mw.unauthorized(writer)
		return
	}

	id := token.Claims["id"].(string)

	if !mw.Authorizator(id, request) {
		mw.unauthorized(writer)
		return
	}

	request.Env["REMOTE_USER"] = id
	handler(writer, request)
}

type login struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

// Handler that clients can use to get jwt token
// Reply will be of the form {"token": "TOKEN"}
func (mw *JWTMiddleware) LoginHandler(writer rest.ResponseWriter, request *rest.Request) {
	login_vals := login{}
	err := request.DecodeJsonPayload(&login_vals)

	if err != nil {
		mw.unauthorized(writer)
		return
	}

	if !mw.Authenticator(login_vals.Username, login_vals.Password) {
		mw.unauthorized(writer)
		return
	}

	token := jwt.New(jwt.GetSigningMethod(mw.SigningAlgorithm))
	token.Claims["id"] = login_vals.Username
	token.Claims["exp"] = time.Now().Add(mw.Timeout).Unix()
	if mw.MaxRefresh != 0 {
		token.Claims["orig_iat"] = time.Now().Unix()
	}
	tokenString, err := token.SignedString(mw.Key)

	if err != nil {
		mw.unauthorized(writer)
		return
	}

	writer.WriteJson(&map[string]string{"token": tokenString})
}

func parseToken(request *rest.Request, key []byte) (*jwt.Token, error) {
	authHeader := request.Header.Get("Authorization")

	if authHeader == "" {
		return nil, errors.New("Auth header empty")
	}

	parts := strings.SplitN(authHeader, " ", 2)
	if !(len(parts) == 2 && parts[0] == "Bearer") {
		return nil, errors.New("Invalid auth header")
	}

	return jwt.Parse(parts[1], func(token *jwt.Token) (interface{}, error) {
		return key, nil
	})
}

type token struct {
	Token string `json:"token"`
}

// Handler that clients can use to refresh their token
// Should be put
// Reply will be of the form {"token": "TOKEN"}
func (mw *JWTMiddleware) RefreshHandler(writer rest.ResponseWriter, request *rest.Request) {
	token, err := parseToken(request, mw.Key)
	origIat := token.Claims["orig_iat"].(int64)

	if origIat < time.Now().Unix() {
		mw.unauthorized(writer)
		return
	}

	newToken := jwt.New(jwt.GetSigningMethod(mw.SigningAlgorithm))
	newToken.Claims["id"] = token.Claims["id"]
	newToken.Claims["exp"] = time.Now().Add(mw.Timeout).Unix()
	newToken.Claims["orig_iat"] = origIat
	tokenString, err := newToken.SignedString(mw.Key)

	if err != nil {
		mw.unauthorized(writer)
		return
	}

	writer.WriteJson(&map[string]string{"token": tokenString})
}

func (mw *JWTMiddleware) unauthorized(writer rest.ResponseWriter) {
	writer.Header().Set("WWW-Authenticate", "Basic realm="+mw.Realm)
	rest.Error(writer, "Not Authorized", http.StatusUnauthorized)
}