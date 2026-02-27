package iris

import (
	"encoding/json"
	"net/http"
	"regexp"
	"strings"
)

const (
	StatusBadRequest = http.StatusBadRequest
	StatusNoContent  = http.StatusNoContent
)

type Handler func(Context)

type route struct {
	method    string
	path      string
	regex     *regexp.Regexp
	paramName string
	handler   Handler
}

type Application struct {
	routes []route
}

func New() *Application { return &Application{} }

func (a *Application) Get(path string, h Handler)  { a.addRoute(http.MethodGet, path, h) }
func (a *Application) Post(path string, h Handler) { a.addRoute(http.MethodPost, path, h) }

func (a *Application) addRoute(method, path string, h Handler) {
	r := route{method: method, path: path, handler: h}
	if strings.Contains(path, "{") && strings.Contains(path, "}") {
		parts := strings.Split(path, "/")
		re := make([]string, 0, len(parts))
		for _, p := range parts {
			if strings.HasPrefix(p, "{") && strings.HasSuffix(p, "}") {
				inside := strings.TrimSuffix(strings.TrimPrefix(p, "{"), "}")
				name := strings.SplitN(inside, ":", 2)[0]
				r.paramName = name
				re = append(re, "([^/]+)")
			} else {
				re = append(re, regexp.QuoteMeta(p))
			}
		}
		r.regex = regexp.MustCompile("^" + strings.Join(re, "/") + "$")
	}
	a.routes = append(a.routes, r)
}

func (a *Application) Listen(addr string) error {
	return http.ListenAndServe(addr, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for _, rt := range a.routes {
			if rt.method != r.Method {
				continue
			}

			params := params{vals: map[string]string{}}
			matched := false
			if rt.regex != nil {
				m := rt.regex.FindStringSubmatch(r.URL.Path)
				if len(m) == 2 {
					params.vals[rt.paramName] = m[1]
					matched = true
				}
			} else if rt.path == r.URL.Path {
				matched = true
			}

			if !matched {
				continue
			}

			ctx := &ctxImpl{w: w, r: r, params: params}
			rt.handler(ctx)
			return
		}
		http.NotFound(w, r)
	}))
}

type Context interface {
	HTML(string)
	JSON(any)
	Params() Params
	FormValue(string) string
	ReadJSON(any) error
	StopWithStatus(int)
	StatusCode(int)
}

type Params interface{ Get(string) string }

type params struct{ vals map[string]string }

func (p params) Get(name string) string { return p.vals[name] }

type ctxImpl struct {
	w      http.ResponseWriter
	r      *http.Request
	params params
}

func (c *ctxImpl) HTML(s string) {
	c.w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = c.w.Write([]byte(s))
}

func (c *ctxImpl) JSON(v any) {
	c.w.Header().Set("Content-Type", "application/json; charset=utf-8")
	_ = json.NewEncoder(c.w).Encode(v)
}

func (c *ctxImpl) Params() Params { return c.params }

func (c *ctxImpl) FormValue(name string) string { return c.r.FormValue(name) }

func (c *ctxImpl) ReadJSON(dest any) error { return json.NewDecoder(c.r.Body).Decode(dest) }

func (c *ctxImpl) StopWithStatus(code int) { c.w.WriteHeader(code) }

func (c *ctxImpl) StatusCode(code int) { c.w.WriteHeader(code) }
