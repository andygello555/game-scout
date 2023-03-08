package api

import (
	"context"
	"fmt"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/machinebox/graphql"
	"github.com/pkg/errors"
	"net/http"
	"reflect"
	"time"
)

// Paginator can fetch resources from a Binding that is paginated. Use NewPaginator or NewTypedPaginator to create a new
// one for a given Binding.
type Paginator[ResT any, RetT any] interface {
	// Continue returns whether the Paginator can continue fetching more pages for the Binding. This will also return true
	// when the Paginator is on the first page.
	Continue() bool
	// Page fetches the current page of results.
	Page() RetT
	// Next fetches the next page from the Binding. The result can be fetched using the Page method.
	Next() error
	// All returns all the return values for the Binding at once.
	All() (RetT, error)
}

type typedPaginator[ResT any, RetT any] struct {
	client      Client
	binding     Binding[ResT, RetT]
	waitTime    time.Duration
	args        []any
	returnType  reflect.Type
	page        int
	currentPage RetT
}

func (p *typedPaginator[ResT, RetT]) Continue() bool {
	return p.page == 1 || reflect.ValueOf(p.currentPage).Len() > 0
}

func (p *typedPaginator[ResT, RetT]) Page() RetT { return p.currentPage }

func (p *typedPaginator[ResT, RetT]) Next() (err error) {
	args := []any{p.page}
	args = append(args, p.args...)
	if p.currentPage, err = p.binding.Execute(p.client, args...); err != nil {
		err = errors.Wrapf(err, "error occurred on page no. %d", p.page)
		return
	}
	p.page++
	if p.waitTime != 0 {
		time.Sleep(p.waitTime)
	}
	return
}

func (p *typedPaginator[ResT, RetT]) All() (RetT, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() {
		if err := p.Next(); err != nil {
			return pages.Interface().(RetT), err
		}
		pages = reflect.AppendSlice(pages, reflect.ValueOf(p.Page()))
	}
	return pages.Interface().(RetT), nil
}

// NewTypedPaginator creates a new type aware Paginator using the given Client, wait time.Duration, and arguments for
// the given Binding. The first given argument should not be the page parameter. Args should contain everything after the
// page parameter that would usually be passed to a standard Binding.Execute.
func NewTypedPaginator[ResT any, RetT any](client Client, waitTime time.Duration, binding Binding[ResT, RetT], args ...any) (paginator Paginator[ResT, RetT], err error) {
	if !binding.Paginated() {
		err = fmt.Errorf("cannot create typed Paginator as Binding is not pagenatable")
		return
	}

	p := &typedPaginator[ResT, RetT]{
		client:   client,
		binding:  binding,
		waitTime: waitTime,
		args:     args,
		page:     1,
	}

	returnType := reflect.ValueOf(new(RetT)).Elem().Type()
	switch returnType.Kind() {
	case reflect.Slice, reflect.Array:
		p.returnType = returnType
		paginator = p
	default:
		err = fmt.Errorf(
			"cannot create typed Paginator for Binding[%v, %v] that has a non-slice/array return type",
			reflect.ValueOf(new(ResT)).Elem().Type(), returnType,
		)
	}
	return
}

type paginator struct {
	client      Client
	binding     *BindingWrapper
	waitTime    time.Duration
	args        []any
	returnType  reflect.Type
	page        int
	currentPage []any
}

func (p *paginator) Continue() bool {
	return p.page == 1 || len(p.currentPage) > 0
}

func (p *paginator) Page() []any { return p.currentPage }

func (p *paginator) Next() (err error) {
	args := []any{p.page}
	args = append(args, p.args...)

	var page any
	if page, err = p.binding.Execute(p.client, args...); err != nil {
		err = errors.Wrapf(err, "error occurred on page no. %d", p.page)
		return
	}

	s := reflect.ValueOf(page)
	p.currentPage = make([]any, 0)
	for i := 0; i < s.Len(); i++ {
		p.currentPage = append(p.currentPage, s.Index(i).Interface())
	}

	p.page++
	if p.waitTime != 0 {
		time.Sleep(p.waitTime)
	}
	return
}

func (p *paginator) All() ([]any, error) {
	pages := make([]any, 0)
	for p.Continue() {
		if err := p.Next(); err != nil {
			return pages, err
		}
		pages = append(pages, p.Page()...)
	}
	return pages, nil
}

func NewPaginator(client Client, waitTime time.Duration, binding BindingWrapper, args ...any) (pag Paginator[[]any, []any], err error) {
	if !binding.Paginated() {
		err = fmt.Errorf("cannot create a Paginator as Binding is not pagenatable")
		return
	}

	p := &paginator{
		client:   client,
		binding:  &binding,
		waitTime: waitTime,
		args:     args,
		page:     1,
	}

	switch binding.returnType.Kind() {
	case reflect.Slice, reflect.Array:
		p.returnType = binding.returnType
		pag = p
	default:
		err = fmt.Errorf(
			"cannot create typed Paginator for Binding[%v, %v] that has a non-slice/array return type",
			binding.responseType, binding.returnType,
		)
	}
	return
}

type BindingRequestMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], args ...any) (request Request)
type BindingResponseWrapperMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], args ...any) (responseWrapper reflect.Value, err error)
type BindingResponseUnwrappedMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], responseWrapper reflect.Value, args ...any) (response ResT, err error)
type BindingResponseMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], response ResT, args ...any) RetT
type BindingExecuteMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], client Client, args ...any) (response RetT, err error)

// Attr is an attribute that can be passed to a Binding when using the NewBinding method. It should return a string key
// and a value.
type Attr func(client Client) (string, any)

type bindingProto[ResT any, RetT any] struct {
	requestMethod           BindingRequestMethod[ResT, RetT]
	responseWrapperMethod   BindingResponseWrapperMethod[ResT, RetT]
	responseUnwrappedMethod BindingResponseUnwrappedMethod[ResT, RetT]
	responseMethod          BindingResponseMethod[ResT, RetT]
	paginated               bool
	attrs                   map[string]any
	attrFuncs               []Attr
}

func (b bindingProto[ResT, RetT]) GetRequestMethod() BindingRequestMethod[ResT, RetT] {
	return b.requestMethod
}
func (b bindingProto[ResT, RetT]) Request(args ...any) (request Request) {
	if b.requestMethod == nil {
		return nil
	}
	return b.requestMethod(b, args...)
}

func (b bindingProto[ResT, RetT]) GetResponseWrapperMethod() BindingResponseWrapperMethod[ResT, RetT] {
	return b.responseWrapperMethod
}
func (b bindingProto[ResT, RetT]) ResponseWrapper(args ...any) (responseWrapper reflect.Value, err error) {
	if b.responseWrapperMethod == nil {
		return reflect.ValueOf(new(ResT)), nil
	}
	return b.responseWrapperMethod(b, args...)
}

func (b bindingProto[ResT, RetT]) GetResponseUnwrappedMethod() BindingResponseUnwrappedMethod[ResT, RetT] {
	return b.responseUnwrappedMethod
}
func (b bindingProto[ResT, RetT]) ResponseUnwrapped(responseWrapper reflect.Value, args ...any) (response ResT, err error) {
	if b.responseUnwrappedMethod == nil {
		return *responseWrapper.Interface().(*ResT), nil
	}
	return b.responseUnwrappedMethod(b, responseWrapper, args...)
}

func (b bindingProto[ResT, RetT]) GetResponseMethod() BindingResponseMethod[ResT, RetT] {
	return b.responseMethod
}
func (b bindingProto[ResT, RetT]) Response(response ResT, args ...any) RetT {
	if b.responseMethod == nil {
		return any(response).(RetT)
	}
	return b.responseMethod(b, response, args...)
}

func (b bindingProto[ResT, RetT]) Execute(client Client, args ...any) (response RetT, err error) {
	b.evaluateAttrs(client)
	req := b.Request(args...)

	var responseWrapper reflect.Value
	if responseWrapper, err = b.ResponseWrapper(args...); err != nil {
		err = errors.Wrapf(err, "could not execute ResponseWrapper for Binding %T", b)
		return
	}
	//log.INFO.Println("execute response wrapper", responseWrapper.Type(), responseWrapper.Elem().Type())
	responseWrapperInt := responseWrapper.Interface()

	ctx := context.Background()
	if err = client.Run(ctx, b.attrs, req, &responseWrapperInt); err != nil {
		err = errors.Wrapf(err, "could not Execute Binding %T", b)
		return
	}

	var responseUnwrapped ResT
	if responseUnwrapped, err = b.ResponseUnwrapped(responseWrapper, args...); err != nil {
		err = errors.Wrapf(err, "could not execute ResponseUnwrapped for Binding %T", b)
		return
	}
	response = b.Response(responseUnwrapped, args...)
	return
}

func (b bindingProto[ResT, RetT]) Paginated() bool       { return b.paginated }
func (b bindingProto[ResT, RetT]) Attrs() map[string]any { return b.attrs }

func (b bindingProto[ResT, RetT]) evaluateAttrs(client Client) {
	evaluate := func(attr Attr) (key string, val any, ok bool) {
		defer func() {
			if p := recover(); p != nil {
				ok = false
			}
		}()
		key, val = attr(client)
		ok = true
		return
	}

	evaluatedAttrIndexes := make([]int, 0)
	for i, attr := range b.attrFuncs {
		key, val, ok := evaluate(attr)
		if ok {
			evaluatedAttrIndexes = append(evaluatedAttrIndexes, i)
			b.attrs[key] = val
		}
	}

	if len(evaluatedAttrIndexes) > 0 {
		b.attrFuncs = slices.RemoveElems(b.attrFuncs, evaluatedAttrIndexes...)
	}
}

// NewBinding creates a new Binding for an API via a prototype that implements the Binding interface. The following
// parameters must be provided:
//
// • request: the method used to construct the Request that will be sent to the API using Client.Run. This will
// implement the Binding.Request method. The function takes the Binding, from which Binding.Attrs can be accessed, as
// well as taking multiple arguments that should be handled accordingly. These are the same arguments passed in from the
// Binding.Execute method. This parameter cannot be supplied a nil-pointer.
//
// • wrap: the method used to construct the wrapper for the response, before it is passed to Client.Run. This will
// implement the Binding.ResponseWrapper method. The function takes the Binding, from which Binding.Attrs can be
// accessed, as well as taking multiple arguments, passed in from Binding.Execute, that can be used in any way the user
// requires. When supplied a nil-pointer, Binding.ResponseWrapper will construct a wrapper instance which is a pointer
// type to ResT.
//
// • unwrap: the method used to unwrap the wrapper instance, constructed by Binding.ResponseWrap, after Client.Run has
// been executed. This will implement the Binding.ResponseUnwrapped method. The function takes the Binding, the response
// wrapper as a reflect.Value instance, and the arguments that were passed into Binding.Execute. When supplied a
// nil-pointer, Binding.ResponseUnwrapped will assert the wrapper instance into *ResT and then return the referenced
// value of this asserted pointer-type. This should only be nil if the wrap argument is also nil.
//
// • response: the method used to convert the response from Binding.ResponseUnwrapped from the type ResT to the type
// RetT. This implements the Binding.Response method. If this is nil, then when executing the Binding.Response method
// the response will be cast to any then asserted into the RetT type. This is useful when the response type is the same
// as the return type.
//
// • paginated: indicates whether this Binding is paginated. If a Binding is paginated, then it can be used with a
// typedPaginator instance to find all/some resources for that Binding. When creating a paginated Binding make sure to bind
// first argument of the request method to be the page number as an int, so that the typedPaginator can feed the page number
// to the Binding appropriately. As well as this, the RetT type must be an array type.
//
// • attrs: the Attr functions used to add attributes to the Binding. These attributes can be retrieved in the
// Binding.Request, Binding.ResponseWrapper, Binding.ResponseUnwrapped, and Binding.Response methods using the
// Binding.Attrs method. Each Attr function is passed the Client instance. NewBinding will initially evaluate each of
// these Attr functions using a null Client. If any Attr functions panic then during this initial evaluation, they will
// be subsequently evaluated in Binding.Execute where they will be passed the Client that is passed to Binding.Execute.
//
// Please see the example for NewAPI on how to use this method practice.
func NewBinding[ResT any, RetT any](
	request BindingRequestMethod[ResT, RetT],
	wrap BindingResponseWrapperMethod[ResT, RetT],
	unwrap BindingResponseUnwrappedMethod[ResT, RetT],
	response BindingResponseMethod[ResT, RetT],
	paginated bool,
	attrs ...Attr,
) Binding[ResT, RetT] {
	b := &bindingProto[ResT, RetT]{
		requestMethod:           request,
		responseWrapperMethod:   wrap,
		responseUnwrappedMethod: unwrap,
		responseMethod:          response,
		paginated:               paginated,
		attrs:                   make(map[string]any),
		attrFuncs:               attrs,
	}
	// We pre-evaluate any attributes that don't need access to the client
	b.evaluateAttrs(nil)
	return b
}

// NewWrappedBinding calls NewBinding then wraps the returned Binding with WrapBinding.
func NewWrappedBinding[ResT any, RetT any](
	name string,
	request BindingRequestMethod[ResT, RetT],
	wrap BindingResponseWrapperMethod[ResT, RetT],
	unwrap BindingResponseUnwrappedMethod[ResT, RetT],
	response BindingResponseMethod[ResT, RetT],
	paginated bool,
	attrs ...Attr,
) BindingWrapper {
	return WrapBinding(name, NewBinding(request, wrap, unwrap, response, paginated, attrs...))
}

// Binding represents an action in an API that can be executed. It takes two type parameters:
//
// • ResT (Response Type): the type used when unmarshalling the response from the API. Be sure to annotate this with the
// correct field tags if necessary.
//
// • RetT (Return Type): the type that will be returned from the Response method. This is the cleaned type that will be
// returned to the user.
//
// To create a new Binding refer to the NewBinding and NewWrappedBinding methods.
type Binding[ResT any, RetT any] interface {
	// Request constructs the Request that will be sent to the API using a Client. The function can take multiple
	// arguments that should be handled accordingly and passed to the Request. These are the same arguments passed in
	// from the Binding.Execute method.
	Request(args ...any) (request Request)
	GetRequestMethod() BindingRequestMethod[ResT, RetT]
	// ResponseWrapper should create a wrapper for the given response type (ResT) and return the pointer reflect.Value to
	// this wrapper. Client.Run will then unmarshal the response into this wrapper instance. This is useful for APIs
	// that return the actual resource (of type ResT) within a structure that is nested within the returned response.
	ResponseWrapper(args ...any) (responseWrapper reflect.Value, err error)
	GetResponseWrapperMethod() BindingResponseWrapperMethod[ResT, RetT]
	// ResponseUnwrapped should unwrap the response that was made to the API after Client.Run. This should return an
	// instance of the ResT type.
	ResponseUnwrapped(responseWrapper reflect.Value, args ...any) (response ResT, err error)
	GetResponseUnwrappedMethod() BindingResponseUnwrappedMethod[ResT, RetT]
	// Response converts the response from the API from the type ResT to the type RetT. It should also be passed
	// additional arguments from Execute.
	Response(response ResT, args ...any) RetT
	GetResponseMethod() BindingResponseMethod[ResT, RetT]
	// Execute will execute the BindingWrapper using the given Client and arguments. It returns the response converted to RetT
	// using the Response method, as well as an error that could have occurred.
	Execute(client Client, args ...any) (response RetT, err error)
	// Paginated returns whether the Binding is paginated.
	Paginated() bool
	// Attrs returns the attributes for the Binding. These can be passed in when creating a Binding through the
	// NewBinding function. Attrs can be used in any of the implemented functions, and they are also passed to
	// Client.Run when Execute-ing the Binding.
	Attrs() map[string]any
}

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
