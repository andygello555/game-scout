package api

import (
	"context"
	"encoding/json"
	"fmt"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/andygello555/gotils/v2/numbers"
	"github.com/andygello555/gotils/v2/slices"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"net/url"
	"reflect"
	"strconv"
	"strings"
	"testing"
)

type httpClient struct {
}

func (h httpClient) Run(ctx context.Context, attrs map[string]any, req Request, res any) (err error) {
	request := req.(HTTPRequest).Request

	var response *http.Response
	if response, err = http.DefaultClient.Do(request); err != nil {
		return err
	}

	if response.Body != nil {
		defer func(body io.ReadCloser) {
			err = myErrors.MergeErrors(err, errors.Wrapf(body.Close(), "could not close response body to %s", request.URL.String()))
		}(response.Body)
	}

	var body []byte
	if body, err = io.ReadAll(response.Body); err != nil {
		err = errors.Wrapf(err, "could not read response body to %s", request.URL.String())
		return
	}

	err = json.Unmarshal(body, res)
	return
}

func TestParams(t *testing.T) {
	var args []any
	var testNo int
	var client Client = httpClient{}

	defer func() {
		if p := recover(); p != nil {
			t.Errorf("panic occurred on test no. %d (%v): %v", testNo, args, p)
		}
	}()
	for _, test := range []struct {
		args     []any
		expected []BindingParam
	}{
		{
			args: []any{"boardIds", []int{}, false, true},
			expected: []BindingParam{
				{
					name:         "boardIds",
					variadic:     true,
					defaultValue: []int{},
					t:            reflect.TypeOf([]int{}),
				},
			},
		},
		{
			args: []any{"boardId", 0, true, "groupId", 0, true, "itemName", "", true, "columnValues", map[string]any{}},
			expected: []BindingParam{
				{
					name:         "boardId",
					required:     true,
					defaultValue: 0,
					t:            reflect.TypeOf(0),
				},
				{
					name:         "groupId",
					required:     true,
					defaultValue: 0,
					t:            reflect.TypeOf(0),
				},
				{
					name:         "itemName",
					required:     true,
					defaultValue: "",
					t:            reflect.TypeOf(""),
				},
				{
					name:         "columnValues",
					defaultValue: map[string]any{},
					t:            reflect.TypeOf(map[string]any{}),
				},
			},
		},
		{
			args: []any{"itemId", 0, true, "msg", "", true},
			expected: []BindingParam{
				{
					name:         "itemId",
					required:     true,
					defaultValue: 0,
					t:            reflect.TypeOf(0),
				},
				{
					name:         "msg",
					required:     true,
					defaultValue: "",
					t:            reflect.TypeOf(""),
				},
			},
		},
		{
			args: []any{"page", 1, "boardIds", []int{}, "groupIds", []int{}},
			expected: []BindingParam{
				{
					name:         "page",
					defaultValue: 1,
					t:            reflect.TypeOf(1),
				},
				{
					name:         "boardIds",
					defaultValue: []int{},
					t:            reflect.TypeOf([]int{}),
				},
				{
					name:         "groupIds",
					defaultValue: []int{},
					t:            reflect.TypeOf([]int{}),
				},
			},
		},
		{
			args: []any{"itemId", 0, true, "boardId", 0, true, "columnValues", map[string]any{}},
			expected: []BindingParam{
				{
					name:         "itemId",
					required:     true,
					defaultValue: 0,
					t:            reflect.TypeOf(0),
				},
				{
					name:         "boardId",
					required:     true,
					defaultValue: 0,
					t:            reflect.TypeOf(0),
				},
				{
					name:         "columnValues",
					defaultValue: map[string]any{},
					t:            reflect.TypeOf(map[string]any{}),
				},
			},
		},
		{
			args: []any{
				"clientInstance", &httpClient{}, true,
				"itemId", 0, true,
				"boardId", 0, true,
				"clientInterfaceRequired", reflect.TypeOf((*Client)(nil)), true,
				"clientInterface", reflect.ValueOf(&client),
			},
			expected: []BindingParam{
				{
					name:         "clientInstance",
					required:     true,
					defaultValue: &httpClient{},
					t:            reflect.TypeOf(&httpClient{}),
				},
				{
					name:         "itemId",
					required:     true,
					defaultValue: 0,
					t:            reflect.TypeOf(0),
				},
				{
					name:         "boardId",
					required:     true,
					defaultValue: 0,
					t:            reflect.TypeOf(0),
				},
				{
					name:          "clientInterfaceRequired",
					required:      true,
					defaultValue:  nil,
					t:             reflect.TypeOf((*Client)(nil)).Elem(),
					interfaceFlag: true,
				},
				{
					name:          "clientInterface",
					defaultValue:  client,
					t:             reflect.TypeOf((*Client)(nil)).Elem(),
					interfaceFlag: true,
				},
			},
		},
		{
			args: []any{
				"typeof(httpClient{})", reflect.TypeOf(httpClient{}),
				"valueof(httpClient{})", reflect.ValueOf(httpClient{}),
				"typeof(&httpClient{})", reflect.TypeOf(&httpClient{}),
				"valueof(&httpClient{})", reflect.ValueOf(&httpClient{}),
			},
			expected: []BindingParam{
				{
					name:         "typeof(httpClient{})",
					defaultValue: nil,
					t:            reflect.TypeOf(httpClient{}),
				},
				{
					name:         "valueof(httpClient{})",
					defaultValue: httpClient{},
					t:            reflect.TypeOf(httpClient{}),
				},
				{
					name:         "typeof(&httpClient{})",
					defaultValue: nil,
					t:            reflect.TypeOf(&httpClient{}),
				},
				{
					name:         "valueof(&httpClient{})",
					defaultValue: &httpClient{},
					t:            reflect.TypeOf(&httpClient{}),
				},
			},
		},
		{
			args: []any{"page", 1, "workspaceIds", []int{}, false, true},
			expected: []BindingParam{
				{
					name:         "page",
					defaultValue: 1,
					t:            reflect.TypeOf(1),
				},
				{
					name:         "workspaceIds",
					variadic:     true,
					defaultValue: []int{},
					t:            reflect.TypeOf([]int{}),
				},
			},
		},
		{
			args: []any{"a", "", true, "b"},
			expected: []BindingParam{
				{
					name:         "a",
					defaultValue: "",
					required:     true,
					t:            reflect.TypeOf(""),
				},
			},
		},
		{
			args: []any{"a", "b", "c"},
			expected: []BindingParam{
				{
					name:         "a",
					defaultValue: "b",
					t:            reflect.TypeOf("b"),
				},
			},
		},
	} {
		args = test.args
		params := Params(test.args...)
		if len(params) != len(test.expected) {
			t.Errorf("test no. %d's expected %d params and not %d params", testNo+1, len(test.expected), len(params))
		} else {
			for i, param := range params {
				if !reflect.DeepEqual(param, test.expected[i]) {
					t.Errorf(
						"test no. %d's %s actual parameter does not match %s expected parameter: %q vs %q",
						testNo+1, numbers.Ordinal(i+1), numbers.Ordinal(i+1), param, test.expected[i],
					)
				}
			}
		}
		testNo++
	}
}

func TestBindingProto_TypeCheckArgs(t *testing.T) {
	var client Client = httpClient{}
	for testNo, test := range []struct {
		params       []BindingParam
		inputArgs    [][]any
		expectedArgs [][]any
		errs         []error
	}{
		{
			params: Params(
				"page", 1, true,
				"a", httpClient{}, true,
				"*a", &httpClient{}, true,
				"typeof(a)", reflect.TypeOf(httpClient{}), true,
				"valueof(a)", reflect.ValueOf(httpClient{}), true,
				"client", reflect.TypeOf((*Client)(nil)), true,
				"greeting", "hello world!",
				"clientDefault", reflect.ValueOf(&client),
				"variadic", []int{}, false, true,
			),
			inputArgs: [][]any{
				// Minimum required arguments
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, httpClient{}},
				// Overriding first non-required argument
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, httpClient{}, "hello golang!"},
				// Overriding second non-required argument
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, httpClient{}, "hello golang!", httpClient{}},
				// Adding variadic arguments to the end
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, httpClient{}, "hello golang!", httpClient{}, 1, 2, 3},
				// Empty arguments will cause an error complaining that the first argument is required but not provided
				{},
				// Changing "valueof(a)" argument to be a pointer type. This should cause an error saying that types are
				// mis-matched.
				{1, httpClient{}, &httpClient{}, &httpClient{}, httpClient{}, httpClient{}},
				// Changing the "client" argument to be a pointer type. Should cause no error because it still
				// implements Client
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, &httpClient{}},
			},
			expectedArgs: [][]any{
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, httpClient{}, "hello world!", client},
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, httpClient{}, "hello golang!", client},
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, httpClient{}, "hello golang!", httpClient{}},
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, httpClient{}, "hello golang!", httpClient{}, 1, 2, 3},
				{},
				{1, httpClient{}, &httpClient{}},
				{1, httpClient{}, &httpClient{}, httpClient{}, httpClient{}, &httpClient{}, "hello world!", client},
			},
			errs: []error{
				errors.New(""),
				errors.New(""),
				errors.New(""),
				errors.New(""),
				errors.New("required param \"page\" (no. 0) was not provided as an argument"),
				errors.New("param \"typeof(a)\"'s type (api.httpClient) does not match arg no. 3's type (*api.httpClient)"),
				errors.New(""),
			},
		},
	} {
		binding := NewBindingChain[bool, bool](func(binding Binding[bool, bool], args ...any) (request Request) {
			return HTTPRequest{nil}
		}).SetParamsMethod(func(binding Binding[bool, bool]) []BindingParam {
			return test.params
		}).(*bindingProto[bool, bool])

		for i, inputArgs := range test.inputArgs {
			actualArgs, err := binding.TypeCheckArgs(inputArgs...)
			if err == nil {
				err = errors.New("")
			}
			if err.Error() != test.errs[i].Error() {
				t.Errorf(
					"test no. %d for input arg set %d raised error \"%v\" when it should have error \"%v\"",
					testNo+1, i+1, err, test.errs[i],
				)
			} else {
				if !reflect.DeepEqual(actualArgs, test.expectedArgs[i]) {
					t.Errorf(
						"test no. %d for input arg set %d expected %v (len %d) args and not %v (len %d) args",
						testNo+1, i+1, test.expectedArgs[i], len(test.expectedArgs[i]), actualArgs, len(actualArgs),
					)
				}
			}
		}
	}
}

func ExampleParams() {
	// Define some types and instance to use in the example...
	type A struct {
		A int
		B int
	}

	// Your interfaces would probably have methods...
	type AInterface interface {
	}
	var aInterfaceInstance AInterface = A{}

	params := Params(
		// Required params should come first...
		// "page" is a required parameter of the type: "int". The type is taken from the "val" argument in the grouping.
		"page", 1, true,
		// "a" is a required parameter of the type: "A"
		"a", A{}, true,
		// "*a" is a required parameter of the type: "*A"
		"*a", &A{}, true,
		// "typeof(a)" is a required parameter of the type: "A", with the default value: "nil".
		"typeof(a)", reflect.TypeOf(A{}), true,
		// "valueof(a)" if the required parameter of the type "A", with the default value: "A". The default value is not
		// used for required params.
		"valueof(a)", reflect.ValueOf(A{}), true,
		// "client" is a required parameter of the type "Client". This is how to pass in arguments of interface types.
		"client", reflect.TypeOf((*Client)(nil)), true,

		// "Non-required params should come after required params..."
		"greeting", "hello world!",
		"interfaceDefault", reflect.ValueOf(&aInterfaceInstance),

		// There can only be one variadic parameter that should come last...
		// "variadic" is a variadic parameter of the type: "int?...". The "required" argument in the grouping will
		// always be set to false by Params.
		"variadic", []int{}, true, true,
	)

	// Print the parameters in a nice comma seperated list
	fmt.Printf("(%s)\n", strings.Join(slices.Comprehension(params, func(idx int, value BindingParam, arr []BindingParam) string {
		return value.String()
	}), ", "))
	// Output:
	// (page: int, a: api.A, *a: *api.A, typeof(a): api.A, valueof(a): api.A, client: [I]api.Client, greeting: string? = "hello world!", clientDefault: [I]api.AInterface? = {0 0}, variadic: []int?... = [])
}

func ExampleNewAPI() {
	// First we need to define our API's response and return structures.
	type Product struct {
		ID          int     `json:"id"`
		Title       string  `json:"title"`
		Price       float64 `json:"price"`
		Category    string  `json:"category"`
		Description string  `json:"description"`
		Image       string  `json:"image"`
	}

	type User struct {
		ID       int    `json:"id"`
		Email    string `json:"email"`
		Username string `json:"username"`
		Password string `json:"password"`
		Name     struct {
			Firstname string `json:"firstname"`
			Lastname  string `json:"lastname"`
		} `json:"name"`
		Address struct {
			City        string `json:"city"`
			Street      string `json:"street"`
			Number      int    `json:"number"`
			Zipcode     string `json:"zipcode"`
			Geolocation struct {
				Lat  string `json:"lat"`
				Long string `json:"long"`
			} `json:"geolocation"`
		} `json:"address"`
		Phone string `json:"phone"`
	}

	// Then we create a Client instance. Here httpClient is a type that implements the Client interface, where
	// Client.Run performs an HTTP request using http.DefaultClient, and then unmarshals the JSON response into the
	// response wrapper.
	client := httpClient{}

	// Finally, we create the API itself by creating and registering all our Bindings within the Schema using the
	// NewWrappedBinding method. The "users" and "products" Bindings take only one argument: the limit argument. This
	// limits the number of resources returned by the fakestoreapi. This is applied to the Request by setting the query
	// params for the http.Request.
	api := NewAPI(client, Schema{
		// Note: we do not supply a wrap and an unwrap method for the "users" and "products" Bindings because the
		//       fakestoreapi returns JSON that can be unmarshalled straight into an appropriate instance of type ResT.
		//       We also don't need to supply a response method because the ResT type is the same as the RetT type.
		"users": NewWrappedBinding("users",
			func(b Binding[[]User, []User], args ...any) (request Request) {
				u, _ := url.Parse("https://fakestoreapi.com/users")
				if len(args) > 0 {
					query := u.Query()
					query.Add("limit", strconv.Itoa(args[0].(int)))
					u.RawQuery = query.Encode()
				}
				req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
				return HTTPRequest{req}
			}, nil, nil, nil,
			func(binding Binding[[]User, []User]) []BindingParam {
				return Params("limit", 1)
			}, false,
		),
		"products": NewWrappedBinding("products",
			func(b Binding[[]Product, []Product], args ...any) Request {
				u, _ := url.Parse("https://fakestoreapi.com/products")
				if len(args) > 0 {
					query := u.Query()
					query.Add("limit", strconv.Itoa(args[0].(int)))
					u.RawQuery = query.Encode()
				}
				req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
				return HTTPRequest{req}
			}, nil, nil, nil,
			func(binding Binding[[]Product, []Product]) []BindingParam {
				return Params("limit", 1)
			}, false,
		),
		// The "first_product" Binding showcases how to set the response method, as well as how to use the chaining API
		// when creating Bindings. This will execute a similar HTTP request to the "products" Binding but
		// Binding.Execute will instead return a single Product instance.
		// Note: how the RetT type param is set to just "Product".
		"first_product": WrapBinding("first_product", NewBindingChain(func(binding Binding[[]Product, Product], args ...any) (request Request) {
			req, _ := http.NewRequest(http.MethodGet, "https://fakestoreapi.com/products?limit=1", nil)
			return HTTPRequest{req}
		}).SetResponseMethod(func(binding Binding[[]Product, Product], response []Product, args ...any) Product {
			return response[0]
		})),
	})

	// Then we can execute our "users" binding with a limit of 3...
	var resp any
	var err error
	if resp, err = api.Execute("users", 3); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(resp.([]User))

	// ...and we can also execute our "products" binding with a limit of 1...
	if resp, err = api.Execute("products", 1); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(resp.([]Product))

	// ...and we can also execute our "first_product" binding.
	if resp, err = api.Execute("first_product"); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(resp.(Product))

	// Finally, we will check whether the "limit" parameter for the "users" action defaults to 1
	if resp, err = api.Execute("users"); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Println(resp.([]User))
	// Output:
	// [{1 john@gmail.com johnd m38rmF$ {john doe} {kilcoole new road 7682 12926-3874 {-37.3159 81.1496}} 1-570-236-7033} {2 morrison@gmail.com mor_2314 83r5^_ {david morrison} {kilcoole Lovers Ln 7267 12926-3874 {-37.3159 81.1496}} 1-570-236-7033} {3 kevin@gmail.com kevinryan kev02937@ {kevin ryan} {Cullman Frances Ct 86 29567-1452 {40.3467 -30.1310}} 1-567-094-1345}]
	// [{1 Fjallraven - Foldsack No. 1 Backpack, Fits 15 Laptops 109.95 men's clothing Your perfect pack for everyday use and walks in the forest. Stash your laptop (up to 15 inches) in the padded sleeve, your everyday https://fakestoreapi.com/img/81fPKd-2AYL._AC_SL1500_.jpg}]
	// {1 Fjallraven - Foldsack No. 1 Backpack, Fits 15 Laptops 109.95 men's clothing Your perfect pack for everyday use and walks in the forest. Stash your laptop (up to 15 inches) in the padded sleeve, your everyday https://fakestoreapi.com/img/81fPKd-2AYL._AC_SL1500_.jpg}
	// [{1 john@gmail.com johnd m38rmF$ {john doe} {kilcoole new road 7682 12926-3874 {-37.3159 81.1496}} 1-570-236-7033}]
}
