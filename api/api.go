package api

import (
	"context"
	"fmt"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/machinebox/graphql"
	"net/http"
	"reflect"
	"sync"
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
	// Run should execute the given Request and unmarshal the response into the given response interface. It is usually
	// called from Binding.Execute to execute a Binding, hence why we also pass in the name of the Binding (from
	// Binding.Name).
	Run(ctx context.Context, bindingName string, attrs map[string]any, req Request, res any) error
}

type RateLimitType int

const (
	// RequestRateLimit means that the RateLimit is limited by the number of HTTP requests that can be made in a certain
	// timespan.
	RequestRateLimit RateLimitType = iota
	// ResourceRateLimit means that the RateLimit is limited by the number of resources that can be fetched in a certain
	// timespan.
	ResourceRateLimit
)

// RateLimit represents a RateLimit for a binding.
type RateLimit interface {
	// Reset returns the time at which the RateLimit resets.
	Reset() time.Time
	// Remaining returns the number of requests remaining/resources that can be fetched for this RateLimit.
	Remaining() int
	// Used returns the number of requests used/resources fetched so far for this RateLimit.
	Used() int
	// Type is the type of the RateLimit. See RateLimitType for documentation.
	Type() RateLimitType
}

// RateLimitedClient is an API Client that has a RateLimit for each Binding it has authority over.
type RateLimitedClient interface {
	// Client should implement a Client.Run method that sets an internal sync.Map of RateLimit(s).
	Client
	// RateLimits returns the sync.Map of Binding names to RateLimit instances.
	RateLimits() *sync.Map
	// AddRateLimit should add a RateLimit to the internal sync.Map within the Client. It should check if the Binding of
	// the given name already has a RateLimit, and whether the RateLimit.Reset lies after the currently set RateLimit
	// for that Binding.
	AddRateLimit(bindingName string, rateLimit RateLimit)
	// LatestRateLimit should return the latest RateLimit for the Binding of the given name. If multiple Binding(s)
	// share the same RateLimit(s) then this can also be encoded into this method.
	LatestRateLimit(bindingName string) RateLimit
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

// Name returns the name of the underlying Binding.
func (bw BindingWrapper) Name() string { return bw.name }

func (bw BindingWrapper) bindingName() string {
	return bw.binding.MethodByName("Name").Call([]reflect.Value{})[0].Interface().(string)
}

// Paginated calls the Binding.Paginated method for the underlying Binding in the BindingWrapper.
func (bw BindingWrapper) Paginated() bool {
	return bw.binding.MethodByName("Paginated").Call([]reflect.Value{})[0].Bool()
}

// Paginator returns an un-typed Paginator for the underlying Binding of the BindingWrapper.
func (bw BindingWrapper) Paginator(client Client, waitTime time.Duration, args ...any) (paginator Paginator[any, any], err error) {
	return NewPaginator(client, waitTime, bw, args...)
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

// Params calls the Binding.Params method for the underlying Binding in the BindingWrapper.
func (bw BindingWrapper) Params() []BindingParam {
	return bw.binding.MethodByName("Params").Call([]reflect.Value{})[0].Interface().([]BindingParam)
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

func (bw BindingWrapper) setName(name string) {
	fmt.Println("setName", bw.binding.Type())
	bw.binding.MethodByName("SetName").Call([]reflect.Value{reflect.ValueOf(name)})
}

// WrapBinding will return the BindingWrapper for the given Binding. The name of the BindingWrapper will be fetched from
// Binding.Name, so make sure to override this before using the Binding.
func WrapBinding[ResT any, RetT any](binding Binding[ResT, RetT]) BindingWrapper {
	var (
		resT ResT
		retT RetT
	)
	return BindingWrapper{
		name:         binding.Name(),
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
	for bindingName, bindingWrapper := range schema {
		bindingWrapper.name = bindingName
	}

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
func (api *API) Paginator(name string, waitTime time.Duration, args ...any) (paginator Paginator[any, any], err error) {
	var binding BindingWrapper
	if binding, err = api.checkBindingExists(name); err != nil {
		return
	}
	return NewPaginator(api.Client, waitTime, binding, args...)
}
