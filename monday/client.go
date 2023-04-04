// Package monday is taken mostly from here: https://go.dev/play/p/aZ7tgaqFxWP.
package monday

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"github.com/andygello555/game-scout/api"
	myErrors "github.com/andygello555/game-scout/errors"
	"github.com/pkg/errors"
	"io"
	"net/http"
	"strconv"
	"time"
)

const endpoint = "https://api.monday.com/v2/"

var DefaultClient api.Client

type User struct {
	Id    int    `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type Board struct {
	Id   string `json:"id"`
	Name string `json:"name"`
}

func (b *Board) ID() int {
	idInt, _ := strconv.ParseInt(b.Id, 10, 64)
	return int(idInt)
}

type Group struct {
	Id    string `json:"id"`
	Title string `json:"title"`
}

type ColumnType string

const (
	TextType           ColumnType = "text"
	StatusType         ColumnType = "color"
	BooleanType        ColumnType = "boolean"
	DateType           ColumnType = "date"
	TimelineType       ColumnType = "timerange"
	MultiplePersonType ColumnType = "multiple-person"
	DropdownType       ColumnType = "dropdown"
	VotesType          ColumnType = "votes"
)

func (ct ColumnType) String() string { return string(ct) }

type Value interface {
	Type() ColumnType
	Value() (any, error)
}

type valueProto struct {
	typeMethod  func() ColumnType
	valueMethod func() (any, error)
}

func (dmp valueProto) Type() ColumnType    { return dmp.typeMethod() }
func (dmp valueProto) Value() (any, error) { return dmp.valueMethod() }

type Column struct {
	Id          string     `json:"id"`
	Title       string     `json:"title"`
	Type        ColumnType `json:"type"`         // text, boolean, color, ...
	Settings    string     `json:"settings_str"` // used to get label index values for color(status) and dropdown column types
	settingsMap map[string]any
}

func (c *Column) SettingsMap() map[string]any {
	if c.settingsMap == nil {
		_ = json.Unmarshal([]byte(c.Settings), &c.settingsMap)
	}
	return c.settingsMap
}

// DecodeLabels displays index value of all labels for a column. Uses column settings_str (see GetColumns and Column).
// Use for Status (color) and Dropdown fields.
func DecodeLabels(column Column, columnValue ColumnValue) Value {
	var err error
	dmp := valueProto{typeMethod: func() ColumnType { return column.Type }}
	labels := make(DropdownMap)

	switch column.Type {
	case StatusType:
		var val StatusIndex
		if err = json.Unmarshal([]byte(columnValue.Value), &val); err != nil {
			err = errors.Wrap(err, "could not unmarshal Status column value")
			break
		}

		var statusLabels struct {
			Labels         map[string]string `json:"labels"`             // index: label
			LabelPositions map[string]int    `json:"label_positions_v2"` // index: position
		}
		if err = json.Unmarshal([]byte(column.Settings), &statusLabels); err != nil {
			err = errors.Wrap(err, "could not unmarshal settings string for Status column")
			break
		}

		fmt.Println(statusLabels)
		fmt.Println(val)
		for index, label := range statusLabels.Labels {
			labels[index] = DropdownValue{
				Name:     label,
				Selected: strconv.Itoa(val.Index) == index,
			}
		}
	case DropdownType:
		var val Dropdown
		if err = json.Unmarshal([]byte(columnValue.Value), &val); err != nil {
			err = errors.Wrap(err, "could not unmarshal Dropdown column value")
			break
		}

		var dropdownLabels struct {
			Labels []struct {
				Id   int    `json:"id"`
				Name string `json:"name"`
			} `json:"labels"`
		}

		if err = json.Unmarshal([]byte(column.Settings), &dropdownLabels); err != nil {
			err = errors.Wrap(err, "could not unmarshal settings string for Dropdown column")
			break
		}

		for _, label := range dropdownLabels.Labels {
			selected := false
			for _, labelValue := range val.Ids {
				if labelValue == label.Id {
					selected = true
					break
				}
			}

			labels[strconv.Itoa(label.Id)] = DropdownValue{
				Name:     label.Name,
				Selected: selected,
			}
		}
	}
	dmp.valueMethod = func() (any, error) { return labels, err }
	return dmp
}

// DecodeValue converts column value returned from Monday to a Value.
//
//	color(status) returns index of label chosen, ex. "3"
//	boolean(checkbox) returns "true" or "false"
//	date returns "2019-05-22"
//
// Types "multi-person" and "dropdown" may have multiple values. For these, a slice of strings is returned
func DecodeValue(columnMap ColumnMap, columnValue ColumnValue) (value Value, err error) {
	column, found := columnMap[columnValue.Id]
	if !found {
		err = fmt.Errorf("invalid column id %q", columnValue.Id)
		return
	}

	if columnValue.Value == "" {
		value = valueProto{
			typeMethod:  func() ColumnType { return column.Type },
			valueMethod: func() (any, error) { return nil, nil },
		}
		return
	}

	inVal := []byte(columnValue.Value) // convert input value (string) to []byte, required by json.Unmarshal
	switch column.Type {
	case TextType:
		value = Text(columnValue.Value)
	case StatusType, DropdownType:
		value = DecodeLabels(column, columnValue)
	case BooleanType: // checkbox, return true or false
		var val Checkbox
		err = json.Unmarshal(inVal, &val)
		value = val
	case DateType:
		var val DateTime
		err = json.Unmarshal(inVal, &val)
		value = val
	case TimelineType:
		var val Timeline
		err = json.Unmarshal(inVal, &val)
		value = val
	case MultiplePersonType:
		var val People
		err = json.Unmarshal(inVal, &val)
		value = val
	case VotesType:
		var val Votes
		err = json.Unmarshal(inVal, &val)
		value = val
	default:
		err = fmt.Errorf("%s value type not handled", column.Type.String())
	}
	return
}

// ColumnMap is a map of column IDs to Column. To fetch the ColumnMap for a specific board/multiple boards you can use
// the GetColumnMap Binding.
type ColumnMap map[string]Column

func (cm ColumnMap) DecodeValue(columnValue ColumnValue) (Value, error) {
	return DecodeValue(cm, columnValue)
}

type ColumnValue struct {
	Id    string `json:"id"` // column id
	Title string `json:"title"`
	Value string `json:"value"` // see func DecodeValue below
}

func (cv ColumnValue) Decode(columnMap ColumnMap) (Value, error) {
	return DecodeValue(columnMap, cv)
}

type Item struct {
	Id           string
	GroupId      string
	BoardId      string
	Name         string
	ColumnValues []ColumnValue
}

type ItemId struct {
	Id string `json:"id"`
}

type Text string

func (t Text) Type() ColumnType    { return TextType }
func (t Text) Value() (any, error) { return string(t), nil }

const (
	DateFormat     = "2006-01-02"
	TimeFormat     = "15:04:05"
	DateTimeFormat = DateFormat + " " + TimeFormat
)

type DateTime struct {
	D string `json:"date"`
	T string `json:"time"`
}

func (dt DateTime) Type() ColumnType { return DateType }
func (dt DateTime) Value() (any, error) {
	d, _ := time.Parse(DateFormat, dt.D)
	t, _ := time.Parse(TimeFormat, dt.T)
	return time.Date(d.Year(), d.Month(), d.Day(), t.Hour(), t.Minute(), t.Second(), 0, time.UTC), nil
}

type Timeline struct {
	From string `json:"from"`
	To   string `json:"to"`
}

func (t Timeline) Type() ColumnType { return TimelineType }
func (t Timeline) Value() (any, error) {
	from, _ := time.Parse(DateFormat, t.From)
	to, _ := time.Parse(DateFormat, t.To)
	return []time.Time{from, to}, nil
}

type DropdownMap map[string]DropdownValue

func (dm DropdownMap) Selected() []DropdownValue {
	values := make([]DropdownValue, 0)
	for _, value := range dm {
		if value.Selected {
			values = append(values, value)
		}
	}
	return values
}

type DropdownValue struct {
	Name     string
	Selected bool
}

type StatusIndex struct {
	Index int `json:"index"`
}

type StatusLabel struct {
	Label string `json:"label"`
}

type Link struct {
	URL  string `json:"url"`
	Text string `json:"text"`
}

type Dropdown struct {
	Ids []int `json:"ids"`
}

type PersonTeam struct {
	Id   int    `json:"id"`
	Kind string `json:"kind"` // "person" or "team"
}

type People struct {
	PersonsAndTeams []PersonTeam `json:"personsAndTeams"`
}

func (p People) Type() ColumnType { return MultiplePersonType }
func (p People) Value() (any, error) {
	result := make([]string, len(p.PersonsAndTeams))
	for i, person := range p.PersonsAndTeams {
		result[i] = strconv.Itoa(person.Id)
	}
	return result, nil
}

type Checkbox struct {
	C string `json:"checked"`
}

func (cb Checkbox) Type() ColumnType { return BooleanType }
func (cb Checkbox) Value() (any, error) {
	return map[string]bool{
		"true":  true,
		"false": false,
	}[cb.C], nil
}

type Votes struct {
	VoterIds  []int  `json:"votersIds"`
	ChangedAt string `json:"changed_at"`
}

func (v Votes) Type() ColumnType    { return VotesType }
func (v Votes) Value() (any, error) { return v.VoterIds, nil }

type MappingConfig interface {
	MappingModelName() string
	MappingBoardIDs() []int
	MappingGroupIDs() []string
	MappingColumnsToUpdate() []string
	MappingModelInstanceIDColumnID() string
	MappingModelInstanceUpvotesColumnID() string
	MappingModelInstanceDownvotesColumnID() string
	MappingModelInstanceWatchedColumnID() string
	ColumnValues(game any, columnIDs ...string) (columnValues map[string]any, err error)
}

type Config interface {
	MondayToken() string
	MondayMappingForModel(model any) MappingConfig
}

// Request is a GraphQL request.
type Request struct {
	q    string
	vars map[string]interface{}

	// Header represent any request headers that will be set
	// when the request is made.
	header http.Header
}

// NewRequest makes a new Request with the specified string.
func NewRequest(q string) *Request {
	req := &Request{
		q:      q,
		header: make(map[string][]string),
	}
	return req
}

func (req *Request) Header() *http.Header {
	return &req.header
}

// Var sets a variable.
func (req *Request) Var(key string, value interface{}) {
	if req.vars == nil {
		req.vars = make(map[string]interface{})
	}
	req.vars[key] = value
}

type Error struct {
	Code        string         `json:"error_code"`
	StatusCode  int            `json:"status_code"`
	Message     string         `json:"error_message"`
	Data        map[string]any `json:"error_data"`
	bindingName string
}

func (e *Error) Error() string {
	return fmt.Sprintf(
		"monday API binding %q returned error %q (%d): %s",
		e.bindingName, e.Code, e.StatusCode, e.Message,
	)
}

type response struct {
	Data interface{}
	*Error
}

type ClientOption func(*Client)

type Client struct {
	Config           Config
	httpClient       *http.Client
	useMultipartForm bool
	Log              func(s string)
}

func (c *Client) logf(format string, args ...interface{}) {
	c.Log(fmt.Sprintf(format, args...))
}

func (c *Client) runWithJSON(ctx context.Context, req *Request, resp any) (err error) {
	var requestBody bytes.Buffer
	requestBodyObj := struct {
		Query     string                 `json:"query"`
		Variables map[string]interface{} `json:"variables"`
	}{
		Query:     req.q,
		Variables: req.vars,
	}

	if err = json.NewEncoder(&requestBody).Encode(requestBodyObj); err != nil {
		err = errors.Wrap(err, "encode body")
		return
	}

	c.logf(">> variables: %v", req.vars)
	c.logf(">> query: %s", req.q)
	gr := &response{
		Data: resp,
	}

	var r *http.Request
	if r, err = http.NewRequest(http.MethodPost, endpoint, &requestBody); err != nil {
		return
	}

	r.Header.Set("Content-Type", "application/json; charset=utf-8")
	r.Header.Set("Accept", "application/json; charset=utf-8")
	for key, values := range *req.Header() {
		for _, value := range values {
			r.Header.Add(key, value)
		}
	}
	c.logf(">> headers: %v", r.Header)

	var res *http.Response
	r = r.WithContext(ctx)
	if res, err = c.httpClient.Do(r); err != nil {
		return
	}

	defer func(Body io.ReadCloser) {
		err = myErrors.MergeErrors(err, Body.Close())
	}(res.Body)

	var buf bytes.Buffer
	if _, err = io.Copy(&buf, res.Body); err != nil {
		err = errors.Wrap(err, "reading body")
		return
	}
	c.logf("<< %s", buf.String())

	if err = json.NewDecoder(&buf).Decode(&gr); err != nil {
		err = errors.Wrap(err, "decoding response")
		return
	}

	if gr.Error != nil {
		err = gr.Error
		return
	}
	return
}

func (c *Client) Run(ctx context.Context, bindingName string, attrs map[string]any, req api.Request, res any) error {
	config := attrs["config"].(Config)
	req.Header().Set("Authorization", config.MondayToken())
	req.Header().Set("Content-Type", "application/json")
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
	}
	return c.runWithJSON(ctx, req.(*Request), res)
}

// CreateClient creates and sets the DefaultClient.
func CreateClient(config Config, opts ...ClientOption) {
	c := &Client{
		Config: config,
		Log:    func(string) {},
	}
	for _, optionFunc := range opts {
		optionFunc(c)
	}
	if c.httpClient == nil {
		c.httpClient = http.DefaultClient
	}
	DefaultClient = c
}

func BuildDate(date string) DateTime {
	return DateTime{D: date}
}

func BuildDateTime(date, time string) DateTime {
	return DateTime{D: date, T: time}
}

func BuildStatusIndex(index int) StatusIndex {
	return StatusIndex{index}
}

func BuildStatusLabel(label string) StatusLabel {
	return StatusLabel{label}
}

func BuildLink(url, text string) Link {
	return Link{url, text}
}

func BuildCheckbox(checked string) Checkbox {
	return Checkbox{checked}
}

func BuildPeople(userIds ...int) People {
	response := People{}
	response.PersonsAndTeams = make([]PersonTeam, len(userIds))
	for i, id := range userIds {
		response.PersonsAndTeams[i] = PersonTeam{id, "person"}
	}
	return response
}
