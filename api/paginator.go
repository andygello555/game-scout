package api

import (
	"fmt"
	"github.com/andygello555/gotils/v2/slices"
	mapset "github.com/deckarep/golang-set/v2"
	"github.com/pkg/errors"
	"reflect"
	"strings"
	"time"
)

// Afterable denotes whether a response type can be used in a Paginator for a Binding that takes an "after" parameter.
type Afterable interface {
	// After returns the value of the "after" parameter that should be used for the next page of pagination. If this
	// returns nil, then it is assumed that pagination has finished.
	After() any
}

type paginatorParamSet int

const (
	unknownParamSet paginatorParamSet = iota
	pageParamSet
	afterParamSet
)

func (pps paginatorParamSet) String() string {
	return strings.TrimPrefix(pps.Set().String(), "Set")
}

func (pps paginatorParamSet) GetPaginatorParamValue(params []BindingParam, resource any, page int) ([]any, error) {
	switch pps {
	case pageParamSet:
		return []any{page}, nil
	case afterParamSet:
		if resource == nil {
			for _, param := range params {
				if param.name == "after" {
					return []any{reflect.Zero(param.Type()).Interface()}, nil
				}
			}
			return nil, fmt.Errorf("cannot find \"after\" parameter in parameters to use zero value for nil resource")
		}

		if afterable, ok := resource.(Afterable); ok {
			return []any{afterable.After()}, nil
		} else {
			return nil, fmt.Errorf("cannot find next \"after\" parameter as return type %T is not Afterable", resource)
		}
	default:
		return nil, fmt.Errorf("%v is not a valid paginatorParamSet", pps)
	}
}

func (pps paginatorParamSet) InsertPaginatorParamValues(params []BindingParam, args []any, paginatorValues []any) []any {
	ppsSet := pps.Set()
	paramIdxs := make([]int, len(paginatorValues))

	i := 0
	for paramNo, param := range params {
		if ppsSet.Contains(param.name) {
			paramIdxs[i] = paramNo
			i++
		}
	}
	args = slices.AddElems(args, paginatorValues, paramIdxs...)
	return args
}

func (pps paginatorParamSet) Set() mapset.Set[string] {
	switch pps {
	case pageParamSet:
		return mapset.NewSet("page")
	case afterParamSet:
		return mapset.NewSet("after")
	default:
		return mapset.NewSet[string]()
	}
}

func (pps paginatorParamSet) Sets() []paginatorParamSet {
	return []paginatorParamSet{pageParamSet, afterParamSet}
}

func checkPaginatorParams(params []BindingParam) paginatorParamSet {
	paramNameSet := mapset.NewSet(slices.Comprehension(params, func(idx int, value BindingParam, arr []BindingParam) string {
		return value.name
	})...)
	for _, pps := range unknownParamSet.Sets() {
		if pps.Set().Difference(paramNameSet).Cardinality() == 0 {
			return pps
		}
	}
	return unknownParamSet
}

var limitParamNames = mapset.NewSet[string]("limit", "count")

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
	// Pages fetches the given number of pages from the Binding whilst appending each response slice together.
	Pages(pages int) (RetT, error)
}

type typedPaginator[ResT any, RetT any] struct {
	client                 Client
	rateLimitedClient      RateLimitedClient
	usingRateLimitedClient bool
	binding                Binding[ResT, RetT]
	params                 []BindingParam
	paramSet               paginatorParamSet
	limitArg               *float64
	waitTime               time.Duration
	args                   []any
	returnType             reflect.Type
	page                   int
	currentPage            RetT
}

func (p *typedPaginator[ResT, RetT]) Continue() bool {
	return p.page == 1 || reflect.ValueOf(p.currentPage).Len() > 0
}

func (p *typedPaginator[ResT, RetT]) Page() RetT { return p.currentPage }

func paginatorCheckRateLimit(
	client Client,
	bindingName string,
	limitArg **float64,
	page int,
	currentPage any,
	params []BindingParam,
	args []any,
) (ignoreFirstRequest bool, ok bool, err error) {
	cont := func() bool {
		return page == 1 || reflect.ValueOf(currentPage).Len() > 0
	}

	var rateLimitedClient RateLimitedClient
	if rateLimitedClient, ok = client.(RateLimitedClient); ok {
		rl := rateLimitedClient.LatestRateLimit(bindingName)
		if rl != nil && rl.Reset().After(time.Now()) {
			sleepTime := rl.Reset().Sub(time.Now())
			switch rl.Type() {
			case RequestRateLimit:
				if rl.Remaining() == 0 {
					time.Sleep(sleepTime)
				}
			case ResourceRateLimit:
				if reflect.ValueOf(currentPage).Len() > rl.Remaining() {
					time.Sleep(sleepTime)
				} else if cont() {
					if limitArg == nil {
						for i, param := range params {
							if !limitParamNames.Contains(param.name) {
								continue
							}

							var argVal reflect.Value
							if i < len(args) {
								argVal = reflect.ValueOf(args[i])
							} else if !param.required && !param.variadic {
								argVal = reflect.ValueOf(param.defaultValue)
							}

							var val float64
							switch {
							case argVal.CanInt():
								val = float64(argVal.Int())
							case argVal.CanUint():
								val = float64(argVal.Uint())
							case argVal.CanFloat():
								val = argVal.Float()
							default:
								continue
							}
							**limitArg = val
							// Break out of the loop if we have found a limit argument
							break
						}
					}

					if **limitArg > float64(rl.Remaining()) {
						time.Sleep(sleepTime)
					}
				}
			}
		} else if page == 1 {
			ignoreFirstRequest = true
		} else {
			err = fmt.Errorf(
				"could not get the latest RateLimit/RateLimit has expired but we are on page %d, check Client.Run",
				page,
			)
			return
		}
	}
	return
}

func (p *typedPaginator[ResT, RetT]) Next() (err error) {
	var paginatorValues []any
	if paginatorValues, err = p.paramSet.GetPaginatorParamValue(p.params, p.currentPage, p.page); err != nil {
		err = errors.Wrapf(
			err, "cannot get paginator param values from %T value on page %d",
			p.currentPage, p.page,
		)
		return
	}
	args := p.paramSet.InsertPaginatorParamValues(p.params, p.args, paginatorValues)

	var ignoreFirstRequest bool
	execute := func() (ret RetT, err error) {
		if ignoreFirstRequest, p.usingRateLimitedClient, err = paginatorCheckRateLimit(
			p.client, p.binding.Name(), &p.limitArg, p.page, p.currentPage, p.params, p.args,
		); err != nil {
			return
		}
		return p.binding.Execute(p.client, args...)
	}

	if p.currentPage, err = execute(); err != nil {
		if !ignoreFirstRequest {
			err = errors.Wrapf(err, "error occurred on page no. %d", p.page)
			return
		}

		if p.currentPage, err = execute(); err != nil {
			err = errors.Wrapf(
				err, "error occurred on page no. %d, after ignoring the first request due to no rate limit",
				p.page,
			)
			return
		}
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

func (p *typedPaginator[ResT, RetT]) Pages(pageNo int) (RetT, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() && p.page <= pageNo {
		if err := p.Next(); err != nil {
			return pages.Interface().(RetT), err
		}
		pages = reflect.AppendSlice(pages, reflect.ValueOf(p.Page()))
	}
	return pages.Interface().(RetT), nil
}

// NewTypedPaginator creates a new type aware Paginator using the given Client, wait time.Duration, and arguments for
// the given Binding. The given Binding's Binding.Paginated method must return true, and the return type (RetT) of the
// Binding must be a slice-type, otherwise an appropriate error will be returned.
//
// The Paginator requires one of the following sets of BindingParam(s) taken by the given Binding:
//  1. ("page",): a singular page argument where each time Paginator.Next is called the page will be incremented
//  2. ("after",): a singular after argument where each time Paginator.Next is called the Afterable.After method will be
//     called on the returned response and the returned value will be set as the "after" parameter for the next
//     Binding.Execute. This requires the RetT to implement the Afterable interface.
//
// The sets of BindingParam(s) shown above are given in priority order. This means that a Binding that defines multiple
// BindingParam(s) that exist within these sets, only the first complete set will be taken.
//
// The args given to NewTypedPaginator should not include the set of BindingParam(s) (listed above), that are going to
// be used to paginate the binding.
//
// If the given Client also implements RateLimitedClient then the given waitTime argument will be ignored in favour of
// waiting (or not) until the RateLimit for the given Binding resets. If the RateLimit that is returned by
// RateLimitedClient.LatestRateLimit is of type ResourceRateLimit, and the Paginator is on the first page. The following
// parameter arguments will be checked for a limit/count value to see whether there is enough RateLimit.Remaining (in
// priority order):
//  1. "limit"
//  2. "count"
func NewTypedPaginator[ResT any, RetT any](client Client, waitTime time.Duration, binding Binding[ResT, RetT], args ...any) (paginator Paginator[ResT, RetT], err error) {
	if !binding.Paginated() {
		err = fmt.Errorf("cannot create typed Paginator as Binding is not pagenatable")
		return
	}

	p := &typedPaginator[ResT, RetT]{
		client:   client,
		binding:  binding,
		params:   binding.Params(),
		waitTime: waitTime,
		args:     args,
		page:     1,
	}

	p.rateLimitedClient, p.usingRateLimitedClient = client.(RateLimitedClient)
	if p.paramSet = checkPaginatorParams(p.params); p.paramSet == unknownParamSet {
		err = fmt.Errorf(
			"cannot create typed Paginator as we couldn't find any paginateable params, need one of the following sets of params %v",
			unknownParamSet.Sets(),
		)
		return
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
	client                 Client
	rateLimitedClient      RateLimitedClient
	usingRateLimitedClient bool
	binding                *BindingWrapper
	params                 []BindingParam
	paramSet               paginatorParamSet
	limitArg               *float64
	waitTime               time.Duration
	args                   []any
	returnType             reflect.Type
	page                   int
	currentPage            any
}

func (p *paginator) Continue() bool { return p.page == 1 || reflect.ValueOf(p.currentPage).Len() > 0 }
func (p *paginator) Page() any      { return p.currentPage }

func (p *paginator) Next() (err error) {
	var paginatorValues []any
	if paginatorValues, err = p.paramSet.GetPaginatorParamValue(p.params, p.currentPage, p.page); err != nil {
		err = errors.Wrapf(
			err, "cannot get paginator param values from %T value on page %d",
			p.currentPage, p.page,
		)
		return
	}
	args := p.paramSet.InsertPaginatorParamValues(p.params, p.args, paginatorValues)
	fmt.Println("paginatorValues", paginatorValues, len(paginatorValues))
	fmt.Println("args", args, len(args))

	var ignoreFirstRequest bool
	execute := func() (err error) {
		if ignoreFirstRequest, p.usingRateLimitedClient, err = paginatorCheckRateLimit(
			p.client, p.binding.Name(), &p.limitArg, p.page, p.currentPage, p.params, p.args,
		); err != nil {
			return
		}

		if p.currentPage, err = p.binding.Execute(p.client, args...); err != nil {
			err = errors.Wrapf(err, "error occurred on page no. %d", p.page)
		}
		return
	}

	if err = execute(); err != nil {
		if !ignoreFirstRequest {
			return
		}

		if err = execute(); err != nil {
			err = errors.Wrapf(
				err, "error occurred on page no. %d, after ignoring the first request due to no rate limit",
				p.page,
			)
			return
		}
	}

	p.page++
	if p.waitTime != 0 {
		time.Sleep(p.waitTime)
	}
	return
}

func (p *paginator) All() (any, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() {
		if err := p.Next(); err != nil {
			return pages, err
		}
		pages = reflect.AppendSlice(pages, reflect.ValueOf(p.Page()))
	}
	return pages.Interface(), nil
}

func (p *paginator) Pages(pageNo int) (any, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() && p.page <= pageNo {
		if err := p.Next(); err != nil {
			return pages, err
		}
		pages = reflect.AppendSlice(pages, reflect.ValueOf(p.Page()))
	}
	return pages.Interface(), nil
}

// NewPaginator creates an un-typed Paginator for the given BindingWrapper. It creates a Paginator in a similar way as
// NewTypedPaginator, except the return type of the Paginator is []any. See NewTypedPaginator for more information on
// Paginator construction.
func NewPaginator(client Client, waitTime time.Duration, binding BindingWrapper, args ...any) (pag Paginator[any, any], err error) {
	if !binding.Paginated() {
		err = fmt.Errorf("cannot create a Paginator as Binding is not pagenatable")
		return
	}

	p := &paginator{
		client:   client,
		binding:  &binding,
		params:   binding.Params(),
		waitTime: waitTime,
		args:     args,
		page:     1,
	}

	p.rateLimitedClient, p.usingRateLimitedClient = client.(RateLimitedClient)
	if p.paramSet = checkPaginatorParams(p.params); p.paramSet == unknownParamSet {
		err = fmt.Errorf(
			"cannot create a Paginator as we couldn't find any paginateable params, need one of the following sets of params %v",
			unknownParamSet.Sets(),
		)
		return
	}

	switch binding.returnType.Kind() {
	case reflect.Slice, reflect.Array:
		p.returnType = binding.returnType
		pag = p
	default:
		err = fmt.Errorf(
			"cannot create a Paginator for Binding[%v, %v] that has a non-slice/array return type",
			binding.responseType, binding.returnType,
		)
	}
	return
}
