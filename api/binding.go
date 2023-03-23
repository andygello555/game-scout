package api

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/andygello555/gotils/v2/numbers"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/pkg/errors"
	"reflect"
	"sync"
)

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
	// GetRequestMethod returns the BindingRequestMethod that is called when Binding.Request is called. This is useful
	// when you want to reuse a BindingRequestMethod for another Binding.
	GetRequestMethod() BindingRequestMethod[ResT, RetT]

	// ResponseWrapper should create a wrapper for the given response type (ResT) and return the pointer reflect.Value to
	// this wrapper. Client.Run will then unmarshal the response into this wrapper instance. This is useful for APIs
	// that return the actual resource (of type ResT) within a structure that is nested within the returned response.
	ResponseWrapper(args ...any) (responseWrapper reflect.Value, err error)
	// GetResponseWrapperMethod returns the BindingResponseWrapperMethod that is called when Binding.Response is called.
	// This is helpful when you want to reuse a BindingResponseWrapperMethod for another Binding.
	GetResponseWrapperMethod() BindingResponseWrapperMethod[ResT, RetT]
	// SetResponseWrapperMethod sets the BindingResponseWrapperMethod that is called when Binding.ResponseWrapper is
	// called. This enables chaining when creating a Binding through NewBindingChain.
	SetResponseWrapperMethod(method BindingResponseWrapperMethod[ResT, RetT]) Binding[ResT, RetT]

	// ResponseUnwrapped should unwrap the response that was made to the API after Client.Run. This should return an
	// instance of the ResT type.
	ResponseUnwrapped(responseWrapper reflect.Value, args ...any) (response ResT, err error)
	// GetResponseUnwrappedMethod returns the BindingResponseUnwrappedMethod that is called when
	// Binding.ResponseUnwrapped is called. This is useful when you want to reuse a BindingResponseUnwrappedMethod for
	// another Binding.
	GetResponseUnwrappedMethod() BindingResponseUnwrappedMethod[ResT, RetT]
	// SetResponseUnwrappedMethod sets the BindingResponseUnwrappedMethod that is called when Binding.ResponseWrapper is
	// called. This enables chaining when creating a Binding through NewBindingChain.
	SetResponseUnwrappedMethod(method BindingResponseUnwrappedMethod[ResT, RetT]) Binding[ResT, RetT]

	// Response converts the response from the API from the type ResT to the type RetT. It should also be passed
	// additional arguments from Execute.
	Response(response ResT, args ...any) RetT
	// GetResponseMethod returns the BindingResponseMethod that is called when Binding.Response is called. This is
	// useful when you want to reuse a BindingResponseMethod for another Binding.
	GetResponseMethod() BindingResponseMethod[ResT, RetT]
	// SetResponseMethod sets the BindingResponseMethod that is called when Binding.Response is called. This enables
	// chaining when creating a Binding through NewBindingChain.
	SetResponseMethod(method BindingResponseMethod[ResT, RetT]) Binding[ResT, RetT]

	// Params returns the BindingParam(s) that this Binding's Execute method takes. These BindingParam(s) are used for
	// type-checking each argument passed to Execute. If no BindingParam(s) are returned by Params, then no
	// type-checking will be performed in Execute.
	Params() []BindingParam
	// GetParamsMethod returns the BindingParamsMethod that is called when Binding.Params is called. This is useful when
	// you want to reuse a BindingParamsMethod for another Binding.
	GetParamsMethod() BindingParamsMethod[ResT, RetT]
	// SetParamsMethod sets the BindingParamsMethod that is called when Binding.Params is called. This enables chaining
	// when creating a Binding through NewBindingChain.
	SetParamsMethod(method BindingParamsMethod[ResT, RetT]) Binding[ResT, RetT]
	// ArgsFromStrings parses the given list of string arguments into their required types for the Params of the
	// Binding.
	ArgsFromStrings(args ...string) ([]any, error)

	// Execute will execute the BindingWrapper using the given Client and arguments. It returns the response converted to RetT
	// using the Response method, as well as an error that could have occurred.
	Execute(client Client, args ...any) (response RetT, err error)

	// Paginated returns whether the Binding is paginated.
	Paginated() bool
	// SetPaginated sets whether the Binding is paginated. It also returns the Binding so that this method can be
	// chained with others when creating a new Binding through NewBindingChain.
	SetPaginated(paginated bool) Binding[ResT, RetT]

	// Name returns the name of the Binding. When using NewBinding, NewBindingChain, or NewWrappedBinding, this will be
	// set to whatever is returned by the following line of code:
	//  fmt.Sprintf("%T", binding)
	// Where "binding" is the referred to Binding.
	Name() string
	// SetName sets the name of the Binding. This returns the Binding so it can be chained.
	SetName(name string) Binding[ResT, RetT]

	// Attrs returns the attributes for the Binding. These can be passed in when creating a Binding through the
	// NewBinding function. Attrs can be used in any of the implemented functions, and they are also passed to
	// Client.Run when Execute-ing the Binding.
	Attrs() map[string]any
	// AddAttrs adds the given Attr functions to the Binding. Each given Attr will attempt to be evaluated when AddAttrs
	// is called. However, when evaluating these Attr functions the Client param will be passed in as nil. If some of
	// these can't be evaluated, due to the lack of the Client param, then these unevaluated Attr functions will also be
	// evaluated at the start of the Binding.Execute method, this time with the Client that is passed to that method.
	AddAttrs(attrs ...Attr) Binding[ResT, RetT]
}

type BindingRequestMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], args ...any) (request Request)
type BindingResponseWrapperMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], args ...any) (responseWrapper reflect.Value, err error)
type BindingResponseUnwrappedMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], responseWrapper reflect.Value, args ...any) (response ResT, err error)
type BindingResponseMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], response ResT, args ...any) RetT
type BindingParamsMethod[ResT any, RetT any] func(binding Binding[ResT, RetT]) []BindingParam
type BindingExecuteMethod[ResT any, RetT any] func(binding Binding[ResT, RetT], client Client, args ...any) (response RetT, err error)

// Attr is an attribute that can be passed to a Binding when using the NewBinding method. It should return a string key
// and a value.
type Attr func(client Client) (string, any)

type bindingProto[ResT any, RetT any] struct {
	requestMethod           BindingRequestMethod[ResT, RetT]
	responseWrapperMethod   BindingResponseWrapperMethod[ResT, RetT]
	responseUnwrappedMethod BindingResponseUnwrappedMethod[ResT, RetT]
	responseMethod          BindingResponseMethod[ResT, RetT]
	paramErr                error
	checkedParams           bool
	paramsMethod            BindingParamsMethod[ResT, RetT]
	paginated               bool
	name                    string
	nameSet                 bool
	attrs                   map[string]any
	attrsMutex              *sync.Mutex
	attrFuncs               []Attr
	attrFuncsMutex          *sync.Mutex
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

func (b bindingProto[ResT, RetT]) SetResponseWrapperMethod(method BindingResponseWrapperMethod[ResT, RetT]) Binding[ResT, RetT] {
	b.responseWrapperMethod = method
	return &b
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

func (b bindingProto[ResT, RetT]) SetResponseUnwrappedMethod(method BindingResponseUnwrappedMethod[ResT, RetT]) Binding[ResT, RetT] {
	b.responseUnwrappedMethod = method
	return &b
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

func (b bindingProto[ResT, RetT]) SetResponseMethod(method BindingResponseMethod[ResT, RetT]) Binding[ResT, RetT] {
	b.responseMethod = method
	return &b
}

func (b bindingProto[ResT, RetT]) Response(response ResT, args ...any) RetT {
	if b.responseMethod == nil {
		return any(response).(RetT)
	}
	return b.responseMethod(b, response, args...)
}

func (b bindingProto[ResT, RetT]) GetParamsMethod() BindingParamsMethod[ResT, RetT] {
	return b.paramsMethod
}

func checkParams(params []BindingParam) (err error) {
	namesToIdx := make(map[string]int)
	lastRequiredParam := 0
	for i, param := range params {
		if sameNameIdx, ok := namesToIdx[param.name]; ok {
			err = fmt.Errorf(
				"param %q (no. %d) has the same name as a previous param: %q (no. %d)",
				param.name, i, param.name, sameNameIdx,
			)
			return
		}
		namesToIdx[param.name] = i

		if param.required {
			if lastRequiredParam <= i-2 {
				err = fmt.Errorf(
					"required param %q (no. %d) cannot come after series of non-required params (i.e. non-reqiured params must be placed after required params)",
					param.name, i,
				)
				return
			}
			lastRequiredParam = i
		} else if param.variadic {
			if i != len(params)-1 {
				err = fmt.Errorf(
					"variadic param %q (no. %d) must be at the end of all parameters (currently lies %s to last)",
					param.name, i, numbers.Ordinal(len(params)-i),
				)
				return
			}

			if param.required {
				err = fmt.Errorf(
					"variadic param %q (no. %d) must not be required",
					param.name, i,
				)
				return
			}

			val := reflect.ValueOf(param.defaultValue)
			if val.Kind() != reflect.Slice && val.Kind() != reflect.Array {
				err = fmt.Errorf(
					"variadic param %q (no. %d)'s default value must be a Slice/Array, not a %s",
					param.name, i, val.Kind(),
				)
				return
			}

			if val.Len() != 0 {
				err = fmt.Errorf(
					"variadic param %q (no. %d)'s default value must be empty and not have %d elems",
					param.name, i, val.Len(),
				)
				return
			}
		}
	}
	return
}

// checkParams will see if the given BindingParam(s) make sense. This means that:
//   - BindingParam(s) should have a unique BindingParam.name.
//   - Non Required BindingParam(s) should trail afterParamSet all Required BindingParam(s).
//   - Variadic BindingParam(s) should trail afterParamSet all non-required BindingParam(s)
//   - Variadic BindingParam(s) should not be Required.
//   - Variadic BindingParam(s) should have DefaultValue that is an empty reflect.Slice/reflect.Array type.
//
// Check BindingParam(s) will not run again if checkedParams is set. If there is an error in the given params then we
// will set the paramErr to an appropriate error which then can be returned in Execute.
func (b bindingProto[ResT, RetT]) checkParams(params []BindingParam) {
	if !b.checkedParams {
		defer func() {
			b.checkedParams = true
		}()
		b.paramErr = checkParams(params)
	}
	return
}

func (b bindingProto[ResT, RetT]) SetParamsMethod(method BindingParamsMethod[ResT, RetT]) Binding[ResT, RetT] {
	b.paramsMethod = method
	// Call the Params method to check the params.
	// Note: we reset the paramErr and checkedParams fields because there is a new method in town!
	b.paramErr = nil
	b.checkedParams = false
	b.Params()
	return &b
}

func (b bindingProto[ResT, RetT]) Params() []BindingParam {
	if b.paramsMethod == nil {
		return []BindingParam{}
	}
	// If there is a params method then we will check if each BindingParam returned by it is in the correct order using
	// checkParams.
	params := b.paramsMethod(b)
	b.checkParams(params)
	return params
}

func (b bindingProto[ResT, RetT]) ArgsFromStrings(args ...string) (parsedArgs []any, err error) {
	params := b.Params()
	if b.paramErr != nil {
		err = b.paramErr
		return
	}

	parsedArgs = make([]any, 0)
	for i, arg := range args {
		param := params[i]
		if param.Type().Kind() == reflect.String {
			arg = fmt.Sprintf("%q", arg)
		}
		val := reflect.New(param.Type())
		if err = json.Unmarshal([]byte(arg), val.Interface()); err != nil {
			err = errors.Wrapf(err, "could not parse arg %q, no. %d, to type %s", arg, i, param.Type())
			return
		}
		parsedArgs = append(parsedArgs, val.Elem().Interface())
	}
	return
}

func (b bindingProto[ResT, RetT]) TypeCheckArgs(args ...any) (newArgs []any, err error) {
	params := b.Params()
	// Check if paramErr was set by checkParams
	if b.paramErr != nil {
		err = b.paramErr
		return
	}

	// Then we get the type info for the params and check them against the given args.
	newArgs = make([]any, 0)
	if len(params) > 0 {
		typeCheck := func(param BindingParam, arg any) (reflect.Type, bool) {
			argType := reflect.TypeOf(arg)
			if param.interfaceFlag {
				return argType, argType.Implements(param.Type())
			} else if param.variadic {
				return argType, param.Type().Elem() == argType
			}
			return argType, argType == param.Type()
		}

		for i, param := range params {
			if i < len(args) {
				// If the parameter is variadic, then we will check if the rest of the arguments all have the same type
				// as the elements that are within the parameter's default value. If everything's ok, then we will exit
				// the loop.
				if param.variadic {
					paramElemType := param.Type().Elem()
					for j, nextArg := range args[i:] {
						if incorrectType, pass := typeCheck(param, nextArg); !pass {
							err = fmt.Errorf(
								"variadic param %q's element type (%s) does not match arg no. %d's type (%s)",
								param.name, paramElemType, j, incorrectType,
							)
							return
						}
						newArgs = append(newArgs, nextArg)
					}
					break
				}

				// If the parameter is non-variadic, then we will check if the argument's type matches the param's type.
				if incorrectType, pass := typeCheck(param, args[i]); !pass {
					err = fmt.Errorf(
						"param %q's type (%s) does not match arg no. %d's type (%s)",
						param.name, param.Type(), i, incorrectType,
					)
					return
				}
				newArgs = append(newArgs, args[i])
			} else {
				if param.required {
					// If the parameter is required but not given, then we will return an error
					err = fmt.Errorf("required param %q (no. %d) was not provided as an argument", param.name, i)
					return
				} else if !param.required && !param.variadic {
					// If the parameter is not required and not variadic, then we will add the default value
					newArgs = append(newArgs, param.defaultValue)
				}
			}
		}
	}
	return
}

func (b bindingProto[ResT, RetT]) Execute(client Client, args ...any) (response RetT, err error) {
	if args, err = b.TypeCheckArgs(args...); err != nil {
		err = errors.Wrapf(err, "type check failed for Binding %T", b)
		return
	}

	b.evaluateAttrs(client)
	req := b.Request(args...)

	var responseWrapper reflect.Value
	if responseWrapper, err = b.ResponseWrapper(args...); err != nil {
		err = errors.Wrapf(err, "could not execute ResponseWrapper for Binding %T", b)
		return
	}
	responseWrapperInt := responseWrapper.Interface()

	ctx := context.Background()
	if err = client.Run(ctx, b.Name(), b.attrs, req, &responseWrapperInt); err != nil {
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
func (b bindingProto[ResT, RetT]) Paginated() bool { return b.paginated }

func (b bindingProto[ResT, RetT]) SetPaginated(paginated bool) Binding[ResT, RetT] {
	b.paginated = paginated
	return &b
}

func (b bindingProto[ResT, RetT]) Name() string {
	if !b.nameSet {
		return fmt.Sprintf("%T", b)
	}
	return b.name
}

func (b bindingProto[ResT, RetT]) SetName(name string) Binding[ResT, RetT] {
	b.name = name
	b.nameSet = true
	return &b
}

func (b bindingProto[ResT, RetT]) Attrs() map[string]any { return b.attrs }

func (b bindingProto[ResT, RetT]) AddAttrs(attrs ...Attr) Binding[ResT, RetT] {
	b.attrFuncsMutex.Lock()
	b.attrFuncs = append(b.attrFuncs, attrs...)
	b.attrFuncsMutex.Unlock()
	b.evaluateAttrs(nil)
	return &b
}

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
			b.attrsMutex.Lock()
			b.attrs[key] = val
			b.attrsMutex.Unlock()
		}
	}

	if len(evaluatedAttrIndexes) > 0 {
		b.attrFuncsMutex.Lock()
		b.attrFuncs = slices.RemoveElems(b.attrFuncs, evaluatedAttrIndexes...)
		b.attrFuncsMutex.Unlock()
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
// • params: the method used to return the BindingParam(s) for type-checking the arguments passed to the Binding.Execute
// method. This implements the Binding.Params method. If this is nil, then Binding.Params will return an empty list of
// BindingParam(s), which disables the type-checking performed in Binding.Execute.
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
	params BindingParamsMethod[ResT, RetT],
	paginated bool,
	attrs ...Attr,
) Binding[ResT, RetT] {
	var attrsMutex, attrFuncsMutex sync.Mutex
	b := &bindingProto[ResT, RetT]{
		requestMethod:           request,
		responseWrapperMethod:   wrap,
		responseUnwrappedMethod: unwrap,
		responseMethod:          response,
		paramsMethod:            params,
		paginated:               paginated,
		attrs:                   make(map[string]any),
		attrsMutex:              &attrsMutex,
		attrFuncs:               attrs,
		attrFuncsMutex:          &attrFuncsMutex,
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
	params BindingParamsMethod[ResT, RetT],
	paginated bool,
	attrs ...Attr,
) BindingWrapper {
	b := NewBinding(request, wrap, unwrap, response, params, paginated, attrs...)
	b.SetName(name)
	return WrapBinding(b)
}

// NewBindingChain creates a new Binding for an API via a prototype that implements the Binding interface. Unlike the
// NewBinding constructor, NewBindingChain takes only a BindingRequestMethod (the other methods for Binding have no
// default implementation) the returned Binding can then have its methods and properties set using the various setters
// available on the Binding interface.
func NewBindingChain[ResT any, RetT any](request BindingRequestMethod[ResT, RetT]) Binding[ResT, RetT] {
	var attrsMutex, attrFuncsMutex sync.Mutex
	b := &bindingProto[ResT, RetT]{
		requestMethod:  request,
		attrs:          make(map[string]any),
		attrsMutex:     &attrsMutex,
		attrFuncs:      make([]Attr, 0),
		attrFuncsMutex: &attrFuncsMutex,
	}
	return b
}
