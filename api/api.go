package api

import (
	"context"
	"fmt"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/machinebox/graphql"
	"net/http"
	"reflect"
	"time"
)

// HTTPRequest is a wrapper for http.Request that implements the Request interface.
type HTTPRequest struct {
	*http.Request
}

func (req HTTPRequest) Header() *http.Header {
	return &req.Request.Header
}

// GraphQLRequest is a wrapper for graphql.Request that implements the Request interface.
type GraphQLRequest struct {
	*graphql.Request
}

func (req GraphQLRequest) Header() *http.Header {
	return &req.Request.Header
}

// Request is the request instance that is constructed by the Binding.Request method.
type Request interface {
	// Header returns a reference to the http.Header for the underlying http.Request that can be modified in any way
	// necessary by Binding.Execute.
	Header() *http.Header
}

// Client is the API client that will execute the Binding.
type Client interface {
	// Run should execute the given Request and unmarshal the response into the given response interface.
	Run(ctx context.Context, attrs map[string]any, req Request, res any) error
}

// BindingWrapper wraps a Binding value with its name. This is used within the Schema map so that we don't have to use
// type parameters everywhere.
type BindingWrapper struct {
	name         string
	responseType reflect.Type
	returnType   reflect.Type
	binding      reflect.Value
}

func (bw BindingWrapper) String() string {
	return fmt.Sprintf("%s/%v", bw.name, bw.binding.Type())
}

// Paginated calls the Binding.Paginated method for the underlying Binding in the BindingWrapper.
func (bw BindingWrapper) Paginated() bool {
	return bw.binding.MethodByName("Paginated").Call([]reflect.Value{})[0].Bool()
}

// ArgsFromStrings calls the Binding.ArgsFromStrings method for the underlying Binding in the BindingWrapper.
func (bw BindingWrapper) ArgsFromStrings(args ...string) (parsedArgs []any, err error) {
	values := bw.binding.MethodByName("ArgsFromStrings").Call(slices.Comprehension(args, func(idx int, value string, arr []string) reflect.Value {
		return reflect.ValueOf(value)
	}))
	parsedArgs = values[0].Interface().([]any)
	err = nil
	if !values[1].IsNil() {
		err = values[1].Interface().(error)
	}
	return
}

// Execute calls the Binding.Execute method for the underlying Binding in the BindingWrapper.
func (bw BindingWrapper) Execute(client Client, args ...any) (val any, err error) {
	arguments := []any{client}
	arguments = append(arguments, args...)
	values := bw.binding.MethodByName("Execute").Call(slices.Comprehension(arguments, func(idx int, value any, arr []any) reflect.Value {
		return reflect.ValueOf(value)
	}))
	val = values[0].Interface()
	err = nil
	if !values[1].IsNil() {
		err = values[1].Interface().(error)
	}
	return
}

// WrapBinding will return the BindingWrapper for the given Binding of the given name.
func WrapBinding[ResT any, RetT any](name string, binding Binding[ResT, RetT]) BindingWrapper {
	var (
		resT ResT
		retT RetT
	)
	return BindingWrapper{
		name:         name,
		responseType: reflect.TypeOf(resT),
		returnType:   reflect.TypeOf(retT),
		binding:      reflect.ValueOf(&binding).Elem(),
	}
}

// Schema is a mapping of names to BindingWrapper(s).
type Schema map[string]BindingWrapper

// API represents a connection to an API with multiple different available Binding(s).
type API struct {
	Client Client
	schema Schema
}

// NewAPI constructs a new API instance for the given Client and Schema combination.
func NewAPI(client Client, schema Schema) *API {
	return &API{
		Client: client,
		schema: schema,
	}
}

// Binding returns the BindingWrapper with the given name in the Schema for this API. The second return value is an "ok"
// flag.
func (api *API) Binding(name string) (BindingWrapper, bool) {
	binding, ok := api.schema[name]
	return binding, ok
}

func (api *API) checkBindingExists(name string) (binding BindingWrapper, err error) {
	var ok bool
	if binding, ok = api.Binding(name); !ok {
		err = fmt.Errorf("could not find Binding for action %q", name)
	}
	return
}

// ArgsFromStrings will execute the Binding.ArgsFromStrings method for the Binding of the given name within the API.
func (api *API) ArgsFromStrings(name string, args ...string) (parsedArgs []any, err error) {
	var binding BindingWrapper
	if binding, err = api.checkBindingExists(name); err != nil {
		return
	}
	return binding.ArgsFromStrings(args...)
}

// Execute will execute the Binding of the given name within the API.
func (api *API) Execute(name string, args ...any) (val any, err error) {
	var binding BindingWrapper
	if binding, err = api.checkBindingExists(name); err != nil {
		return
	}
	return binding.Execute(api.Client, args...)
}

// Paginator returns a Paginator for the Binding of the given name within the API.
func (api *API) Paginator(name string, waitTime time.Duration, args ...any) (paginator Paginator[[]any, []any], err error) {
	var binding BindingWrapper
	if binding, err = api.checkBindingExists(name); err != nil {
		return
	}
	return NewPaginator(api.Client, waitTime, binding, args...)
}
