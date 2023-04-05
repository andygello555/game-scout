package monday

import (
	"encoding/json"
	"fmt"
	"github.com/andygello555/game-scout/api"
	"github.com/andygello555/gotils/v2/slices"
	"reflect"
)

func ResponseWrapper[ResT any, RetT any](binding api.Binding[ResT, RetT], args ...any) (responseWrapper reflect.Value, err error) {
	responseWrapperType := reflect.StructOf([]reflect.StructField{
		{
			Name: "Resource",
			Type: reflect.ValueOf(new(ResT)).Elem().Type(),
			Tag:  reflect.StructTag(fmt.Sprintf(`json:"%s"`, binding.Attrs()["jsonResponseKey"].(string))),
		},
	})
	return reflect.New(responseWrapperType), nil
}

func ResponseUnwrapped[ResT any, RetT any](binding api.Binding[ResT, RetT], responseWrapper reflect.Value, args ...any) (response ResT, err error) {
	return responseWrapper.Elem().Field(0).Interface().(ResT), nil
}

// Me is an api.Binding that will retrieve the User bound to the Monday API token used to fetch it.
var Me = api.NewBinding[User, User](
	func(binding api.Binding[User, User], args ...any) (request api.Request) {
		return NewRequest(`{ me { id name email } }`)
	},
	ResponseWrapper[User, User],
	ResponseUnwrapped[User, User],
	nil, nil, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "me" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("Me")

// GetUsers is a Binding to retrieve all User from the linked Monday.com organisation for the API token.
var GetUsers = api.NewBinding[[]User, []User](
	func(binding api.Binding[[]User, []User], args ...any) api.Request {
		return NewRequest(`{ users { id name email } }`)
	},
	ResponseWrapper[[]User, []User],
	ResponseUnwrapped[[]User, []User],
	nil, nil, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "users" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("GetUsers")

// GetBoards is a Binding to retrieve multiple Board from the given workspaces. Arguments provided to
// Binding.Execute:
//
// • page (int): The page of results to retrieve. This means that GetBoards can be passed to an api.Paginator.
//
// • workspaceIds ([]int...): The IDs of the workspaces to retrieve all Board from.
//
// Binding.Execute returns a list of Board instances from the given workspaces for the given page of results.
var GetBoards = api.NewBinding[[]Board, []Board](
	func(b api.Binding[[]Board, []Board], args ...any) api.Request {
		var (
			page         int
			workspaceIds []int
		)
		req := NewRequest(`query ($page: Int!, $workspaceIds: [Int]) { boards (page: $page, workspace_ids: $workspaceIds) { id name } }`)
		page = args[0].(int)
		workspaceIds = slices.Comprehension[any, int](args[1:], func(idx int, value any, arr []any) int {
			return value.(int)
		})
		req.Var("page", page)
		req.Var("workspaceIds", workspaceIds)
		return req
	},
	ResponseWrapper[[]Board, []Board],
	ResponseUnwrapped[[]Board, []Board],
	nil, func(binding api.Binding[[]Board, []Board]) []api.BindingParam {
		return api.Params("page", 1, "workspaceIds", []int{}, false, true)
	}, true,
	func(client api.Client) (string, any) { return "jsonResponseKey", "boards" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("GetBoards")

type groupResponse []struct {
	Groups []Group `json:"groups"`
}

func boardIdsParam() []api.BindingParam {
	return api.Params("boardIds", []int{}, false, true)
}

// GetGroups is a Binding to retrieve multiple Group from the given boards. Arguments provided to Binding.Execute:
//
// • boardIds ([]int...): The IDs of the boards to retrieve all Group from.
//
// Binding.Execute returns a list of Group instances from the given boards.
var GetGroups = api.NewBinding[groupResponse, []Group](
	func(b api.Binding[groupResponse, []Group], args ...any) api.Request {
		req := NewRequest(`query ($boardIds: [Int]) { boards (ids: $boardIds) { groups { id title } } }`)
		req.Var("boardIds", slices.Comprehension(args, func(idx int, value any, arr []any) int {
			return value.(int)
		}))
		return req
	},
	ResponseWrapper[groupResponse, []Group],
	ResponseUnwrapped[groupResponse, []Group],
	func(b api.Binding[groupResponse, []Group], response groupResponse, args ...any) []Group {
		groups := make([]Group, 0)
		for _, board := range response {
			groups = append(groups, board.Groups...)
		}
		return groups
	},
	func(binding api.Binding[groupResponse, []Group]) []api.BindingParam {
		return boardIdsParam()
	}, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "boards" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("GetGroups")

type columnResponse []struct {
	Id      string   `json:"id"`
	Columns []Column `json:"columns"`
}

func getColumnsRequest[ResT any, RetT any](b api.Binding[ResT, RetT], args ...any) api.Request {
	req := NewRequest(`query ($boardIds: [Int]) { boards (ids: $boardIds) { id columns {id title type settings_str} } }`)
	req.Var("boardIds", slices.Comprehension(args, func(idx int, value any, arr []any) int {
		return value.(int)
	}))
	return req
}

// GetColumns is a Binding to retrieve multiple Column from the given boards. Arguments provided to Binding.Execute:
//
// • boardIds ([]int...): The IDs of the boards to retrieve all Column from.
//
// Binding.Execute returns a list of Column instances from the given boards.
var GetColumns = api.NewBinding[columnResponse, []Column](
	getColumnsRequest[columnResponse, []Column],
	ResponseWrapper[columnResponse, []Column],
	ResponseUnwrapped[columnResponse, []Column],
	func(b api.Binding[columnResponse, []Column], response columnResponse, args ...any) []Column {
		columns := make([]Column, 0)
		for _, board := range response {
			columns = append(columns, board.Columns...)
		}
		return columns
	},
	func(binding api.Binding[columnResponse, []Column]) []api.BindingParam {
		return boardIdsParam()
	}, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "boards" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("GetColumns")

// GetColumnMap is a Binding to retrieve the multiple ColumnMap from the given boards. Arguments provided to
// Binding.Execute:
//
// • boardIds ([]int...): The IDs of the boards to retrieve all ColumnMap for.
//
// Binding.Execute returns a map of Board IDs to ColumnMap instances for the given boards.
var GetColumnMap = api.NewBinding[columnResponse, map[string]ColumnMap](
	getColumnsRequest[columnResponse, map[string]ColumnMap],
	ResponseWrapper[columnResponse, map[string]ColumnMap],
	ResponseUnwrapped[columnResponse, map[string]ColumnMap],
	func(b api.Binding[columnResponse, map[string]ColumnMap], response columnResponse, args ...any) map[string]ColumnMap {
		m := make(map[string]ColumnMap)
		for _, board := range response {
			m[board.Id] = make(ColumnMap)
			for _, column := range board.Columns {
				m[board.Id][column.Id] = column
			}
		}
		return m
	},
	func(binding api.Binding[columnResponse, map[string]ColumnMap]) []api.BindingParam {
		return boardIdsParam()
	}, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "boards" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("GetColumnMap")

// Example of creating columnValues for AddItem
// map entry key is column id; run GetColumns to get column id's
/*
	columnValues := map[string]interface{}{
		"text":   "have a nice day",
		"date":   monday.BuildDate("2019-05-22"),
		"status": monday.BuildStatusIndex(2),
		"people": monday.BuildPeople(123456, 987654),   // parameters are user ids
	}
*/

// AddItem is a Binding that adds an Item of the given name and column values to the given Board and Group combination.
// Arguments provided to Binding.Execute:
//
// • boardId (int): The ID of the Board to which to add the Item to.
//
// • groupId (string): The ID of the Group to which to add the Item to.
//
// • itemName (string): The name of the new Item.
//
// • columnValues (map[string]interface{}): The values of the columns of the new Item.
//
// Binding.Execute returns the ID of the newly added Item.
var AddItem = api.NewBinding[ItemId, string](
	func(b api.Binding[ItemId, string], args ...any) api.Request {
		req := NewRequest(`mutation ($boardId: Int!, $groupId: String!, $itemName: String!, $colValues: JSON!) { create_item (board_id: $boardId, group_id: $groupId, item_name: $itemName, column_values: $colValues ) { id } }`)
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
	ResponseWrapper[ItemId, string],
	ResponseUnwrapped[ItemId, string],
	func(b api.Binding[ItemId, string], response ItemId, args ...any) string {
		return response.Id
	},
	func(binding api.Binding[ItemId, string]) []api.BindingParam {
		return api.Params("boardId", 0, true, "groupId", 0, true, "itemName", "", true, "columnValues", map[string]any{})
	}, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "create_item" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("AddItem")

// AddItemUpdate is a Binding that adds an update with the given message to the Item of the given ID. Arguments provided
// to Binding.Execute:
//
// • itemId (int): The ID of the Item to add an update to.
//
// • msg (string): The body of the update that will be added to the Item.
//
// Binding.Execute returns the ID of the Item to which the update was added.
var AddItemUpdate = api.NewBinding[ItemId, string](
	func(b api.Binding[ItemId, string], args ...any) api.Request {
		itemId := args[0].(int)
		msg := args[1].(string)
		req := NewRequest(`mutation ($itemId: Int!, $body: String!) { create_update (item_id: $itemId, body: $body ) { id } }`)
		req.Var("itemId", itemId)
		req.Var("body", msg)
		return req
	},
	ResponseWrapper[ItemId, string],
	ResponseUnwrapped[ItemId, string],
	func(b api.Binding[ItemId, string], response ItemId, args ...any) string {
		return response.Id
	},
	func(binding api.Binding[ItemId, string]) []api.BindingParam {
		return api.Params("itemId", 0, true, "msg", "", true)
	}, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "create_update" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("AddItemUpdate")

type ItemResponse []struct {
	Id     string `json:"id"`
	Groups []struct {
		Id    string `json:"id"`
		Items []struct {
			Id           string        `json:"id"`
			Name         string        `json:"name"`
			ColumnValues []ColumnValue `json:"column_values"`
		} `json:"items"`
	} `json:"groups"`
}

// GetItems is a Binding that retrieves multiple Item from the given Board(s) and Group(s). Arguments provided to
// Binding.Execute:
//
// • page (int): The page of results to retrieve. This means that GetItems can be passed to an api.Paginator.
//
// • boardIds ([]int): The IDs of the Board from which to retrieve all Item from.
//
// • groupIds ([]string): The IDs of the Group from which to retrieve all Item from.
//
// Binding.Execute returns a list of Item instances from the given Board(s) and Group(s).
var GetItems = api.NewBinding[ItemResponse, []Item](
	func(b api.Binding[ItemResponse, []Item], args ...any) api.Request {
		var (
			page     int
			boardIds []int
			groupIds []string
		)
		req := NewRequest(`
			query ($page: Int!, $boardIds: [Int], $groupIds: [String]) {
				boards (ids: $boardIds) {
					id
					groups (ids: $groupIds) {
						id
						items (limit: 10, page: $page) {
							id name
							column_values { id title value }
						}
					}
				}
			}
		`)
		if len(args) > 0 {
			page = args[0].(int)
		}
		if len(args) > 1 {
			boardIds = args[1].([]int)
		}
		if len(args) > 2 {
			groupIds = args[2].([]string)
		}
		req.Var("page", page)
		req.Var("boardIds", boardIds)
		req.Var("groupIds", groupIds)
		return req
	},
	ResponseWrapper[ItemResponse, []Item],
	ResponseUnwrapped[ItemResponse, []Item],
	func(b api.Binding[ItemResponse, []Item], response ItemResponse, args ...any) []Item {
		items := make([]Item, 0)
		for _, board := range response {
			for _, group := range board.Groups {
				for _, item := range group.Items {
					items = append(items, Item{
						Id:           item.Id,
						GroupId:      group.Id,
						BoardId:      board.Id,
						Name:         item.Name,
						ColumnValues: item.ColumnValues,
					})
				}
			}
		}
		return items
	},
	func(binding api.Binding[ItemResponse, []Item]) []api.BindingParam {
		return api.Params("page", 1, "boardIds", []int{}, "groupIds", []string{})
	}, true,
	func(client api.Client) (string, any) { return "jsonResponseKey", "boards" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("GetItems")

// ChangeMultipleColumnValues is a Binding that changes multiple ColumnValue for the given Item and Board combination.
// Arguments provided to Binding.Execute:
//
// • itemId (int): The ID of the Item for which we want to change the column values for.
//
// • boardId (int): The ID of the Board that the Item resides within.
//
// • columnValues (map[string]interface{}): The values to set the columns of the Item of the given ID that is within the
// Board of the given ID.
//
// Binding.Execute returns the ID of the Item that has been mutated.
var ChangeMultipleColumnValues = api.NewBinding[ItemId, string](
	func(b api.Binding[ItemId, string], args ...any) api.Request {
		req := NewRequest(`mutation ($itemId: Int, $boardId: Int!, $colValues: JSON!) { change_multiple_column_values (item_id: $itemId, board_id: $boardId, column_values: $colValues) { id } }`)
		itemId := args[0].(int)
		boardId := args[1].(int)
		columnValues := args[2].(map[string]interface{})
		jsonValues, _ := json.Marshal(&columnValues)
		req.Var("itemId", itemId)
		req.Var("boardId", boardId)
		req.Var("colValues", string(jsonValues))
		return req
	},
	ResponseWrapper[ItemId, string],
	ResponseUnwrapped[ItemId, string],
	func(b api.Binding[ItemId, string], response ItemId, args ...any) string {
		return response.Id
	},
	func(binding api.Binding[ItemId, string]) []api.BindingParam {
		return api.Params("itemId", 0, true, "boardId", 0, true, "columnValues", map[string]any{})
	}, false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "change_multiple_column_values" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
).SetName("ChangeMultipleColumnValues")

// DeleteItem is an api.Binding that deletes the Item of the given item ID. Arguments provided to Binding.Execute:
//
// itemId (int): The ID of the Item that we want to delete.
//
// Binding.Execute returns the ID for the Item that was deleted.
var DeleteItem = api.NewBinding[ItemId, string](
	func(binding api.Binding[ItemId, string], args ...any) (request api.Request) {
		req := NewRequest(`mutation ($itemId: Int) { delete_item (item_id: $itemId) { id } }`)
		itemId := args[0].(int)
		req.Var("itemId", itemId)
		return req
	},
	ResponseWrapper[ItemId, string],
	ResponseUnwrapped[ItemId, string],
	func(binding api.Binding[ItemId, string], response ItemId, args ...any) string { return response.Id },
	func(binding api.Binding[ItemId, string]) []api.BindingParam { return api.Params("itemId", 0, true) },
	false,
	func(client api.Client) (string, any) { return "jsonResponseKey", "delete_item" },
	func(client api.Client) (string, any) { return "config", client.(*Client).Config },
)
