package api

import (
	"context"
	"encoding/json"
	"fmt"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
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
		"users": NewWrappedBinding[[]User, []User]("users",
			func(b Binding[[]User, []User], args ...any) (request Request) {
				u, _ := url.Parse("https://fakestoreapi.com/users")
				if len(args) > 0 {
					query := u.Query()
					query.Add("limit", strconv.Itoa(args[0].(int)))
					u.RawQuery = query.Encode()
				}
				req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
				return HTTPRequest{req}
			}, nil, nil, nil, false,
		),
		"products": NewWrappedBinding[[]Product, []Product]("products",
			func(b Binding[[]Product, []Product], args ...any) Request {
				u, _ := url.Parse("https://fakestoreapi.com/products")
				if len(args) > 0 {
					query := u.Query()
					query.Add("limit", strconv.Itoa(args[0].(int)))
					u.RawQuery = query.Encode()
				}
				req, _ := http.NewRequest(http.MethodGet, u.String(), nil)
				return HTTPRequest{req}
			}, nil, nil, nil, false,
		),
		// The "first_product" Binding showcases how to set the response method. This will execute a similar HTTP request
		// to the "products" Binding but Binding.Execute will instead return a single Product instance.
		// Note: how the RetT type param is set to Product.
		"first_product": NewWrappedBinding[[]Product, Product]("first_product",
			func(b Binding[[]Product, Product], args ...any) Request {
				req, _ := http.NewRequest(http.MethodGet, "https://fakestoreapi.com/products?limit=1", nil)
				return HTTPRequest{req}
			}, nil, nil,
			func(b Binding[[]Product, Product], response []Product, args ...any) Product {
				return response[0]
			}, false,
		),
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
	// Output:
	// [{1 john@gmail.com johnd m38rmF$ {john doe} {kilcoole new road 7682 12926-3874 {-37.3159 81.1496}} 1-570-236-7033} {2 morrison@gmail.com mor_2314 83r5^_ {david morrison} {kilcoole Lovers Ln 7267 12926-3874 {-37.3159 81.1496}} 1-570-236-7033} {3 kevin@gmail.com kevinryan kev02937@ {kevin ryan} {Cullman Frances Ct 86 29567-1452 {40.3467 -30.1310}} 1-567-094-1345}]
	// [{1 Fjallraven - Foldsack No. 1 Backpack, Fits 15 Laptops 109.95 men's clothing Your perfect pack for everyday use and walks in the forest. Stash your laptop (up to 15 inches) in the padded sleeve, your everyday https://fakestoreapi.com/img/81fPKd-2AYL._AC_SL1500_.jpg}]
	// {1 Fjallraven - Foldsack No. 1 Backpack, Fits 15 Laptops 109.95 men's clothing Your perfect pack for everyday use and walks in the forest. Stash your laptop (up to 15 inches) in the padded sleeve, your everyday https://fakestoreapi.com/img/81fPKd-2AYL._AC_SL1500_.jpg}
}
