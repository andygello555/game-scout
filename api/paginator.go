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

// Mergeable denotes whether a return type can be merged in a Paginator for a Binding. Instances of Mergeable can be
// used instead of reflect.Slice or reflect.Array types as a return type for a Paginator.
type Mergeable interface {
	// Merge merges the given value into the Mergeable instance.
	Merge(similar any) error
	// HasMore returns true if there are more pages to fetch.
	HasMore() bool
}

// Lenable is an interface that provides the Len interface for calculating the length of things.
type Lenable interface {
	Len() int
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

func (pps paginatorParamSet) GetPaginatorParamValue(params []BindingParam, resource any, page int) (map[string]any, error) {
	switch pps {
	case pageParamSet:
		return map[string]any{"page": page}, nil
	case afterParamSet:
		if resource == nil {
			for _, param := range params {
				if param.name == "after" {
					return map[string]any{"after": reflect.Zero(param.Type()).Interface()}, nil
				}
			}
			return nil, fmt.Errorf("cannot find \"after\" parameter in parameters to use zero value for nil resource")
		}

		if afterable, ok := resource.(Afterable); ok {
			return map[string]any{"after": afterable.After()}, nil
		} else {
			return nil, fmt.Errorf("cannot find next \"after\" parameter as return type %T is not Afterable", resource)
		}
	default:
		return nil, fmt.Errorf("%v is not a valid paginatorParamSet", pps)
	}
}

func (pps paginatorParamSet) InsertPaginatorParamValues(params []BindingParam, args []any, paginatorValues map[string]any) ([]any, error) {
	ppsSet := pps.Set()
	ppsSetUsed := mapset.NewSet[string]()

	// Here we find out what indices that the paginator args should be passed in relative to the ordering of the params.
	for paramNo, param := range params {
		if pagVal, ok := paginatorValues[param.name]; ok {
			args = slices.AddElems(args, []any{pagVal}, paramNo)
			ppsSetUsed.Add(param.name)
		} else if paramNo >= len(args) {
			if param.required {
				return args, fmt.Errorf(
					"required parameter %q (no. %d) cannot be defaulted and not all paginator args have been inserted yet (%s remaining)",
					param.name, paramNo, ppsSet.Difference(ppsSetUsed),
				)
			} else {
				args = slices.AddElems(args, []any{param.defaultValue}, paramNo)
			}
		}

		// If we have marked all paginator arguments to be inserted then we can break out of the loop
		if ppsSet.Equal(ppsSetUsed) {
			break
		}
	}
	return args, nil
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
	// Until keeps fetching pages until there are no more pages, or the given predicate function returns false.
	Until(predicate func(paginator Paginator[ResT, RetT], pages RetT) bool) (RetT, error)
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

func (p *typedPaginator[ResT, RetT]) mergeable() bool {
	return p.returnType.Implements(reflect.TypeOf((*Mergeable)(nil)).Elem())
}

func (p *typedPaginator[ResT, RetT]) Continue() bool {
	hasMore := false
	if p.returnType.Implements(reflect.TypeOf((*Mergeable)(nil)).Elem()) {
		if mergeable, ok := any(p.currentPage).(Mergeable); ok {
			hasMore = mergeable.HasMore()
		}
	} else {
		hasMore = reflect.ValueOf(p.currentPage).Len() > 0
	}
	return p.page == 1 || hasMore
}

func (p *typedPaginator[ResT, RetT]) Page() RetT { return p.currentPage }

func paginatorCheckRateLimit(
	client Client,
	waitTime time.Duration,
	bindingName string,
	limitArg **float64,
	page int,
	currentPage any,
	params []BindingParam,
	args []any,
) (ignoreFirstRequest bool, ok bool, err error) {
	var rateLimitedClient RateLimitedClient
	if rateLimitedClient, ok = client.(RateLimitedClient); ok {
		rl := rateLimitedClient.LatestRateLimit(bindingName)
		tries := 3
		for rl == nil && tries > 0 {
			rateLimitedClient.Log(fmt.Sprintf(
				"Could not get latest rate limit for %q%v on page no. %d. Trying again in %s (%d tries left)...",
				bindingName, args, page, waitTime.String(), tries,
			))
			time.Sleep(waitTime)
			rl = rateLimitedClient.LatestRateLimit(bindingName)
			tries--
		}

		if rl != nil && rl.Reset().After(time.Now().UTC()) {
			sleepTime := rl.Reset().Sub(time.Now().UTC())
			switch rl.Type() {
			case RequestRateLimit:
				if rl.Remaining() == 0 {
					rateLimitedClient.Log(fmt.Sprintf(
						"Latest request rate limit for %q%v has expired on page no. %d. Sleeping for %s until %s...",
						bindingName, args, page, sleepTime.String(), rl.Reset(),
					))
					time.Sleep(sleepTime)
				}
			case ResourceRateLimit:
				cont := func() bool {
					return page == 1 || reflect.ValueOf(currentPage).Len() > 0
				}

				if reflect.ValueOf(currentPage).Len() > rl.Remaining() {
					rateLimitedClient.Log(fmt.Sprintf(
						"Latest resource rate limit for %q%v has expired on page no. %d. Sleeping for %s until %s...",
						bindingName, args, page, sleepTime.String(), rl.Reset(),
					))
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
						rateLimitedClient.Log(fmt.Sprintf(
							"Latest resource rate limit for %q%v has expired on page no. %d. Sleeping for %s until %s...",
							bindingName, args, page, sleepTime.String(), rl.Reset(),
						))
						time.Sleep(sleepTime)
					}
				}
			}
		} else if page == 1 {
			ignoreFirstRequest = true
		} else if rl == nil {
			rateLimitedClient.Log(fmt.Sprintf(
				"Could not get the latest rate limit for %q%v on page no. %d",
				bindingName, args, page,
			))
			err = fmt.Errorf(
				"could not get the latest RateLimit/RateLimit has expired but we are on page %d, check Client.Run",
				page,
			)
			return
		} else {
			rateLimitedClient.Log(fmt.Sprintf(
				"Latest rate limit for %q is before the current time: %s - %s = %s, so we are going to execute the binding anyway",
				bindingName, time.Now().UTC().Format("15:04:05"), rl.Reset().Format("15:04:05"), time.Now().UTC().Sub(rl.Reset()),
			))
		}
	}
	return
}

func (p *typedPaginator[ResT, RetT]) Next() (err error) {
	var paginatorValues map[string]any
	if paginatorValues, err = p.paramSet.GetPaginatorParamValue(p.params, p.currentPage, p.page); err != nil {
		err = errors.Wrapf(
			err, "cannot get paginator param values from %T value on page %d",
			p.currentPage, p.page,
		)
		return
	}

	var args []any
	if args, err = p.paramSet.InsertPaginatorParamValues(p.params, p.args, paginatorValues); err != nil {
		err = errors.Wrapf(
			err, "cannot insert paginator values (%v) into arguments for page %d",
			paginatorValues, p.page,
		)
	}

	var ignoreFirstRequest bool
	execute := func() (ret RetT, err error) {
		if ignoreFirstRequest, p.usingRateLimitedClient, err = paginatorCheckRateLimit(
			p.client, p.waitTime, p.binding.Name(), &p.limitArg, p.page, p.currentPage, p.params, p.args,
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

func (p *typedPaginator[ResT, RetT]) merge(pages reflect.Value) (reflect.Value, error) {
	mergeable := p.mergeable()
	if mergeable {
		if p.page == 2 {
			pages = reflect.ValueOf(p.currentPage)
		} else {
			if err := pages.Interface().(Mergeable).Merge(p.Page()); err != nil {
				return pages, err
			}
		}
	} else {
		pages = reflect.AppendSlice(pages, reflect.ValueOf(p.Page()))
	}
	return pages, nil
}

func (p *typedPaginator[ResT, RetT]) All() (RetT, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() {
		var err error
		// Fetch the next page...
		if err = p.Next(); err != nil {
			return pages.Interface().(RetT), err
		}

		// ...merge the current page into the aggregation of all pages
		if pages, err = p.merge(pages); err != nil {
			return pages.Interface().(RetT), err
		}
	}
	return pages.Interface().(RetT), nil
}

func (p *typedPaginator[ResT, RetT]) Pages(pageNo int) (RetT, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() && p.page <= pageNo {
		var err error
		// Fetch the next page...
		if err = p.Next(); err != nil {
			return pages.Interface().(RetT), err
		}

		// ...merge the current page into the aggregation of all pages
		if pages, err = p.merge(pages); err != nil {
			return pages.Interface().(RetT), err
		}
	}
	return pages.Interface().(RetT), nil
}

func (p *typedPaginator[ResT, RetT]) Until(predicate func(paginator Paginator[ResT, RetT], pages RetT) bool) (RetT, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() && predicate(p, pages.Interface().(RetT)) {
		var err error
		// Fetch the next page...
		if err = p.Next(); err != nil {
			return pages.Interface().(RetT), err
		}

		// ...merge the current page into the aggregation of all pages
		if pages, err = p.merge(pages); err != nil {
			return pages.Interface().(RetT), err
		}
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
	if returnType.Implements(reflect.TypeOf((*Mergeable)(nil)).Elem()) {
		p.returnType = returnType
	} else {
		switch returnType.Kind() {
		case reflect.Slice, reflect.Array:
			p.returnType = returnType
		default:
			err = fmt.Errorf(
				"cannot create typed Paginator for Binding[%v, %v] that has a non-slice/array return type",
				reflect.ValueOf(new(ResT)).Elem().Type(), returnType,
			)
			return
		}
	}
	paginator = p
	return
}

// MustTypePaginate calls NewTypedPaginator with the given arguments and panics if an error occurs.
func MustTypePaginate[ResT any, RetT any](client Client, waitTime time.Duration, binding Binding[ResT, RetT], args ...any) (paginator Paginator[ResT, RetT]) {
	var err error
	if paginator, err = NewTypedPaginator(client, waitTime, binding, args...); err != nil {
		panic(err)
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

func (p *paginator) mergeable() bool {
	return p.returnType.Implements(reflect.TypeOf((*Mergeable)(nil)).Elem())
}

func (p *paginator) Continue() bool {
	hasMore := false
	if p.returnType.Implements(reflect.TypeOf((*Mergeable)(nil)).Elem()) {
		if mergeable, ok := p.currentPage.(Mergeable); ok {
			hasMore = mergeable.HasMore()
		}
	} else {
		hasMore = reflect.ValueOf(p.currentPage).Len() > 0
	}
	return p.page == 1 || hasMore
}

func (p *paginator) Page() any { return p.currentPage }

func (p *paginator) Next() (err error) {
	var paginatorValues map[string]any
	if paginatorValues, err = p.paramSet.GetPaginatorParamValue(p.params, p.currentPage, p.page); err != nil {
		err = errors.Wrapf(
			err, "cannot get paginator param values from %T value on page %d",
			p.currentPage, p.page,
		)
		return
	}

	var args []any
	if args, err = p.paramSet.InsertPaginatorParamValues(p.params, p.args, paginatorValues); err != nil {
		err = errors.Wrapf(
			err, "cannot insert paginator values (%v) into arguments for page %d",
			paginatorValues, p.page,
		)
	}
	//fmt.Println("paginatorValues", paginatorValues, len(paginatorValues))
	//fmt.Println("args", args, len(args))

	var ignoreFirstRequest bool
	execute := func() (err error) {
		if ignoreFirstRequest, p.usingRateLimitedClient, err = paginatorCheckRateLimit(
			p.client, p.waitTime, p.binding.Name(), &p.limitArg, p.page, p.currentPage, p.params, p.args,
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

func (p *paginator) merge(pages reflect.Value) (reflect.Value, error) {
	mergeable := p.mergeable()
	if mergeable {
		// If we have just fetched the first page then we will set pages to be the value of the first page
		if p.page == 2 {
			pages = reflect.ValueOf(p.currentPage)
		} else {
			if err := pages.Interface().(Mergeable).Merge(p.Page()); err != nil {
				return reflect.ValueOf(nil), err
			}
		}
	} else {
		pages = reflect.AppendSlice(pages, reflect.ValueOf(p.Page()))
	}
	return pages, nil
}

func (p *paginator) All() (any, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() {
		var err error
		// Fetch the next page...
		if err = p.Next(); err != nil {
			return pages.Interface(), err
		}

		// ...merge the current page into the aggregation of all pages
		if pages, err = p.merge(pages); err != nil {
			return pages.Interface(), err
		}
	}
	return pages.Interface(), nil
}

func (p *paginator) Pages(pageNo int) (any, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() && p.page <= pageNo {
		var err error
		// Fetch the next page...
		if err = p.Next(); err != nil {
			return pages.Interface(), err
		}

		// ...merge the current page into the aggregation of all pages
		if pages, err = p.merge(pages); err != nil {
			return pages.Interface(), err
		}
	}
	return pages.Interface(), nil
}

func (p *paginator) Until(predicate func(paginator Paginator[any, any], pages any) bool) (any, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() && predicate(p, pages.Interface()) {
		var err error
		// Fetch the next page...
		if err = p.Next(); err != nil {
			return pages.Interface(), err
		}

		// ...merge the current page into the aggregation of all pages
		if pages, err = p.merge(pages); err != nil {
			return pages.Interface(), err
		}
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

	if binding.returnType.Implements(reflect.TypeOf((*Mergeable)(nil)).Elem()) {
		p.returnType = binding.returnType
	} else {
		switch binding.returnType.Kind() {
		case reflect.Slice, reflect.Array:
			p.returnType = binding.returnType
		default:
			err = fmt.Errorf(
				"cannot create a Paginator for Binding[%v, %v] that has a non-slice/array return type",
				binding.responseType, binding.returnType,
			)
			return
		}
	}
	pag = p
	return
}

// MustPaginate calls NewPaginator for the given arguments and panics if an error occurs.
func MustPaginate(client Client, waitTime time.Duration, binding BindingWrapper, args ...any) (paginator Paginator[any, any]) {
	var err error
	if paginator, err = NewPaginator(client, waitTime, binding, args...); err != nil {
		panic(err)
	}
	return
}
