package api

import (
	"fmt"
	"github.com/pkg/errors"
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
