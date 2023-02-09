package monday

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/machinebox/graphql"
	"github.com/pkg/errors"
	"reflect"
	"time"
)

type Paginator[ResT any, RetT any] struct {
	client      *Client
	binding     Binding[ResT, RetT]
	waitTime    time.Duration
	args        []any
	returnType  reflect.Type
	page        int
	currentPage RetT
}

func NewPaginator[ResT any, RetT any](client *Client, waitTime time.Duration, binding Binding[ResT, RetT], args ...any) (paginator *Paginator[ResT, RetT], err error) {
	if !binding.(bindingProto[ResT, RetT]).paginated {
		err = fmt.Errorf("cannot create Paginator as Binding is not pagenatable")
		return
	}

	paginator = &Paginator[ResT, RetT]{
		client:   client,
		binding:  binding,
		waitTime: waitTime,
		args:     args,
		page:     1,
	}

	returnType := reflect.ValueOf(new(RetT)).Elem().Type()
	switch returnType.Kind() {
	case reflect.Slice, reflect.Array:
		paginator.returnType = returnType
	default:
		err = fmt.Errorf(
			"cannot create Paginator for Binding[%v, %v] that has a non-slice/array return type",
			reflect.ValueOf(new(ResT)).Elem().Type(), returnType,
		)
	}
	return
}

// Continue returns whether the Paginator can continue fetching more pages for the Binding. This will also return true
// when the Paginator is on the first page.
func (p *Paginator[ResT, RetT]) Continue() bool {
	return p.page == 1 || reflect.ValueOf(p.currentPage).Len() > 0
}

// Page fetches the current page of results.
func (p *Paginator[ResT, RetT]) Page() RetT { return p.currentPage }

// Next fetches the next page from the Binding. The result can be fetched using the Page method.
func (p *Paginator[ResT, RetT]) Next() (err error) {
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

// All returns all the return values for the Binding at once.
func (p *Paginator[ResT, RetT]) All() (RetT, error) {
	pages := reflect.New(p.returnType).Elem()
	for p.Continue() {
		if err := p.Next(); err != nil {
			return pages.Interface().(RetT), err
		}
		pages = reflect.AppendSlice(pages, reflect.ValueOf(p.Page()))
	}
	return pages.Interface().(RetT), nil
}

// Binding represents an action on the Monday GraphQL API that can be executed. It takes two type parameters:
//
// • ResT: the type used when unmarshalling the JSON response from Monday API. Be sure to annotate this with the correct
// field tags.
//
// • RetT: the type that will be returned from the bindingProto.Execute method. This is the cleaned type that will be
// returned to the user.
//
// To create a new Binding use the NewBinding function. Make sure to set the prototype's methods accordingly.
type Binding[ResT any, RetT any] interface {
	// Request constructs the graphql.Request that will be sent to the Monday API using a Client. The function can take
	// multiple arguments that should be handled accordingly and passed to the graphql.Request. These are the same
	// arguments passed in from the Binding.Execute method.
	Request(args ...any) (request *graphql.Request)
	// Response converts the response from the Monday API from the type ResT to the type RetT.
	Response(response ResT) RetT
	// Execute will execute the binding using the given Client and arguments. It returns the response converted to RetT
	// using the Response method, as well as an error that could have occurred.
	Execute(client *Client, args ...any) (response RetT, err error)
}

// bindingProto is the prototype for Binding.
type bindingProto[ResT, RetT any] struct {
	jsonResponseKey string
	paginated       bool
	requestMethod   func(args ...any) *graphql.Request
	responseMethod  func(response ResT) RetT
}

// NewBinding creates a new Binding for the Monday API via a prototype that implements the Binding interface. The
// following parameters must be provided:
//
// • request: the method used to construct the graphql.Request that will be sent to the Monday API using a Client. This
// implements the Binding.Request method. The function can take multiple arguments that should be handled accordingly
// and passed to the graphql.Request. These are the same arguments passed in from the Binding.Execute method.
//
// • response: the method used to convert the response from the Monday API from the type ResT to the type RetT. This
// implements the Binding.Response method. If this is nil, then when executing the Binding.Response method the response
// will be cast to any then asserted into the RetT type. The use case for this is where the response type is the same as
// the return type.
//
// • jsonResponseKey: the JSON key that the ResT instance will be a value of in the JSON response returned by the Monday
// API. For instance, in the GetBoards Binding we set jsonResponseKey to be "boards" as this is the key whose value
// contains an array of Board.
//
// • paginated: indicates whether the action is paginated. If a Binding is paginated, then it can be used with a
// Paginator instance to find all/some resources for that Binding. When creating a paginated Binding make sure to bind
// first argument of the request method to be the page number as an int, so that the Paginator can feed the page number
// to the Binding appropriately. As well as this, the RetT type must be an array type.
func NewBinding[ResT any, RetT any](request func(args ...any) *graphql.Request, response func(response ResT) RetT, jsonResponseKey string, paginated bool) Binding[ResT, RetT] {
	return bindingProto[ResT, RetT]{
		jsonResponseKey: jsonResponseKey,
		paginated:       paginated,
		requestMethod:   request,
		responseMethod:  response,
	}
}

func (bp bindingProto[ResT, RetT]) Request(args ...any) *graphql.Request {
	return bp.requestMethod(args...)
}

func (bp bindingProto[ResT, RetT]) Response(response ResT) RetT {
	if bp.responseMethod == nil {
		return any(response).(RetT)
	}
	return bp.responseMethod(response)
}

func (bp bindingProto[ResT, RetT]) Execute(client *Client, args ...any) (response RetT, err error) {
	req := bp.Request(args...)
	responseWrapperType := reflect.StructOf([]reflect.StructField{
		{
			Name: "Resource",
			Type: reflect.ValueOf(new(ResT)).Elem().Type(),
			Tag:  reflect.StructTag(fmt.Sprintf(`json:"%s"`, bp.jsonResponseKey)),
		},
	})
	responseWrapper := reflect.New(responseWrapperType)
	responseWrapperInt := responseWrapper.Interface()

	req.Header.Set("Authorization", client.Config.MondayToken())
	req.Header.Set("Content-Type", "application/json")
	ctx := context.Background()
	if err = client.Run(ctx, req, &responseWrapperInt); err != nil {
		err = errors.Wrapf(err, "Could not Execute Monday binding %T", bp)
	} else {
		response = bp.Response(responseWrapper.Elem().Field(0).Interface().(ResT))
	}
	return
}

// GetUsers returns an array of User that are currently subscribed to the same organisation as the requester.
var GetUsers = NewBinding[[]User, []User](
	func(args ...any) *graphql.Request {
		return graphql.NewRequest(`{ users { id name email } }`)
	},
	nil, "users", false,
)

var GetBoards = NewBinding[[]Board, []Board](
	func(args ...any) *graphql.Request {
		var (
			page         int
			workspaceIds []int
		)
		req := graphql.NewRequest(`query ($page: Int!, $workspaceIds: [Int]) { boards (page: $page, workspace_ids: $workspaceIds) { id name } }`)
		if len(args) > 0 {
			page = args[0].(int)
		}
		if len(args) > 1 {
			workspaceIds = slices.Comprehension[any, int](args[1:], func(idx int, value any, arr []any) int {
				return value.(int)
			})
		}
		req.Var("page", page)
		req.Var("workspaceIds", workspaceIds)
		return req
	},
	nil, "boards", true,
)

var GetGroups = NewBinding[[]struct {
	Groups []Group `json:"groups"`
}, []Group](
	func(args ...any) *graphql.Request {
		req := graphql.NewRequest(`query ($boardIds: [Int]) { boards (ids: $boardIds) { groups { id title } } }`)
		req.Var("boardIds", slices.Comprehension(args, func(idx int, value any, arr []any) int {
			return value.(int)
		}))
		return req
	},
	func(response []struct {
		Groups []Group `json:"groups"`
	}) []Group {
		groups := make([]Group, 0)
		for _, board := range response {
			groups = append(groups, board.Groups...)
		}
		return groups
	},
	"boards", false,
)

type columnResponse []struct {
	Id      string   `json:"id"`
	Columns []Column `json:"columns"`
}

func getColumnsRequest(args ...any) *graphql.Request {
	req := graphql.NewRequest(`query ($boardIds: [Int]) { boards (ids: $boardIds) { id columns {id title type settings_str} } }`)
	req.Var("boardIds", slices.Comprehension(args, func(idx int, value any, arr []any) int {
		return value.(int)
	}))
	return req
}

var GetColumns = NewBinding[columnResponse, []Column](
	getColumnsRequest,
	func(response columnResponse) []Column {
		columns := make([]Column, 0)
		for _, board := range response {
			columns = append(columns, board.Columns...)
		}
		return columns
	}, "boards", false,
)

var GetColumnMap = NewBinding[columnResponse, map[string]ColumnMap](
	getColumnsRequest,
	func(response columnResponse) map[string]ColumnMap {
		m := make(map[string]ColumnMap)
		for _, board := range response {
			m[board.Id] = make(ColumnMap)
			for _, column := range board.Columns {
				m[board.Id][column.Id] = column
			}
		}
		return m
	}, "boards", false,
)

var AddItem = NewBinding[ItemId, string](
	func(args ...any) *graphql.Request {
		req := graphql.NewRequest(`mutation ($boardId: Int!, $groupId: String!, $itemName: String!, $colValues: JSON!) { create_item (board_id: $boardId, group_id: $groupId, item_name: $itemName, column_values: $colValues ) { id } }`)
		boardId := args[0].(int)
		groupId := args[1].(string)
		itemName := args[2].(string)
		columnValues := args[3].(map[string]interface{})
		jsonValues, _ := json.Marshal(&columnValues)
		req.Var("boardId", boardId)
		req.Var("groupId", groupId)
		req.Var("itemName", itemName)
		req.Var("colValues", string(jsonValues))
		return req
	},
	func(response ItemId) string {
		return response.Id
	}, "create_item", false,
)

var AddItemUpdate = NewBinding[ItemId, string](
	func(args ...any) *graphql.Request {
		itemId := args[0].(int)
		msg := args[1].(string)
		req := graphql.NewRequest(`mutation ($itemId: Int!, $body: String!) { create_update (item_id: $itemId, body: $body ) { id } }`)
		req.Var("itemId", itemId)
		req.Var("body", msg)
		return req
	},
	func(response ItemId) string {
		return response.Id
	}, "create_update", false,
)

type itemResponse []struct {
	Items []struct {
		Id    string `json:"id"`
		Group struct {
			Id string `json:"id"`
		} `json:"group"`
		Name         string        `json:"name"`
		ColumnValues []ColumnValue `json:"column_values"`
	} `json:"items"`
}

var GetItems = NewBinding[itemResponse, []Item](
	func(args ...any) *graphql.Request {
		var (
			page     int
			boardIds []int
		)
		req := graphql.NewRequest(`
			query ($page: Int!, $boardIds: [Int]) {
				boards (ids: $boardIds){
					items (limit: 10, page: $page) {
						id group { id } name
						column_values {
							id title value
						}
					}
				}
			}
		`)
		if len(args) > 0 {
			page = args[0].(int)
		}
		if len(args) > 1 {
			boardIds = slices.Comprehension[any, int](args[1:], func(idx int, value any, arr []any) int {
				return value.(int)
			})
		}
		req.Var("page", page)
		req.Var("boardIds", boardIds)
		return req
	},
	func(response itemResponse) []Item {
		items := make([]Item, 0)
		for _, board := range response {
			for _, item := range board.Items {
				items = append(items, Item{
					Id:           item.Id,
					GroupId:      item.Group.Id,
					Name:         item.Name,
					ColumnValues: item.ColumnValues,
				})
			}
		}
		return items
	}, "boards", true,
)
