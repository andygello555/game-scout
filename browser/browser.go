package browser

import (
	"fmt"
	"github.com/pkg/errors"
	"github.com/playwright-community/playwright-go"
	"strings"
)

var UserAgent = "Mozilla/5.0 (X11; Ubuntu; Linux x86_64; rv:90.0) Gecko/20100101 Firefox/90.0"

// CommandResult is the instance that is returned by the methods that evaluate a Command.
type CommandResult struct {
	// Elements are the elements found by the previous Command. This will be overwritten when a Command that finds
	// elements finds new elements.
	Elements []playwright.ElementHandle
	// InnerTexts are the results of the InnerText CommandType. These will be accumulated over the lifespan of the
	// CommandResult.
	InnerTexts []string
}

// NewCommandResult creates a new CommandResult.
func NewCommandResult() *CommandResult {
	return &CommandResult{
		Elements:   make([]playwright.ElementHandle, 0),
		InnerTexts: make([]string, 0),
	}
}

// CommandResultFrom creates a new CommandResult from an existing CommandResult copying the old Elements and InnerTexts
// by reference.
func CommandResultFrom(result *CommandResult) *CommandResult {
	return &CommandResult{
		Elements:   result.Elements,
		InnerTexts: result.InnerTexts,
	}
}

// CommandType represents the type of Command.
type CommandType int

const (
	// Select a single element using a selector stored in the Command.Value field and store it in the
	// CommandResult.Elements field.
	Select CommandType = iota
	// Type a string of text into the first element that is stored within the CommandResult.Elements field.
	Type
	// SelectAll will select all the elements that satisfy the selector in Command.Value and store it in the
	// CommandResult.Elements field.
	SelectAll
	// Find will run the Command.FindFunc against all elements that exist within the CommandResult.Elements field and
	// store each element that satisfies this predicate within the CommandResult.Elements field (overwriting the
	// previous).
	Find
	// FindInnerText will run the Command.FindFunc against the InnerText of all the elements that exist within the
	// CommandResult.Elements field and store each element that satisfies this predicate within the
	// CommandResult.Elements field (overwriting the previous).
	FindInnerText
	// Click the first element in the CommandResult.Elements field.
	Click
	// InnerText will find the innerText of all the elements in the CommandResult.Elements field and store them in the
	// CommandResult.InnerTexts field. Remember that CommandResult.InnerTexts is an accumulator that will not be cleared
	// until the user clears it for themselves.
	InnerText
	// Goto will browse to the URL given in Command.Value.
	Goto
)

// Execute will execute the CommandType when given a playwright.Page, a Command, and a CommandResult containing the
// previous result of a Command. Look at the CommandType enumeration to see the mutations upon each of these inputs and
// outputs.
func (ct CommandType) Execute(page playwright.Page, command *Command, previousResult *CommandResult) (result *CommandResult, err error) {
	result = CommandResultFrom(previousResult)
	switch ct {
	case Select:
		result.Elements = make([]playwright.ElementHandle, 1)
		if command.Wait {
			options := make([]playwright.PageWaitForSelectorOptions, len(command.Options))
			if len(command.Options) > 0 {
				for i, opt := range command.Options {
					options[i] = opt.(playwright.PageWaitForSelectorOptions)
				}
			}
			result.Elements[0], err = page.WaitForSelector(command.Value, options...)
		} else {
			result.Elements[0], err = page.QuerySelector(command.Value)
		}
	case SelectAll:
		result.Elements, err = page.QuerySelectorAll(command.Value)
	case Find:
		result.Elements = make([]playwright.ElementHandle, 0)
		for _, element := range previousResult.Elements {
			if command.FindFunc(element) {
				result.Elements = append(result.Elements, element)
			}
		}
	case FindInnerText:
		result.Elements = make([]playwright.ElementHandle, 0)
		for i, element := range previousResult.Elements {
			text := ""
			if text, err = element.InnerText(); err != nil {
				err = errors.Wrap(err, fmt.Sprintf("could not get the InnerText of element %d", i))
				break
			} else {
				if command.FindFunc(text) {
					result.Elements = append(result.Elements, element)
				}
			}
		}
	case Type:
		options := make([]playwright.ElementHandleTypeOptions, len(command.Options))
		if len(command.Options) > 0 {
			for i, opt := range command.Options {
				options[i] = opt.(playwright.ElementHandleTypeOptions)
			}
		}
		if err = previousResult.Elements[0].Type(command.Value, options...); err != nil {
			err = errors.Wrap(err, fmt.Sprintf("cannot type \"%s\" into the first element", command.Value))
		}
	case Click:
		options := make([]playwright.ElementHandleClickOptions, len(command.Options))
		if len(command.Options) > 0 {
			for i, opt := range command.Options {
				options[i] = opt.(playwright.ElementHandleClickOptions)
			}
		}
		if err = previousResult.Elements[0].Click(options...); err != nil {
			err = errors.Wrap(err, "cannot click on the first element")
		}
	case InnerText:
		for i, element := range previousResult.Elements {
			text := ""
			if text, err = element.InnerText(); err != nil {
				err = errors.Wrap(err, fmt.Sprintf("could not get the InnerText of element %d", i))
				break
			} else {
				result.InnerTexts = append(result.InnerTexts, text)
			}
		}
	case Goto:
		options := make([]playwright.PageGotoOptions, len(command.Options))
		if len(command.Options) > 0 {
			for i, opt := range command.Options {
				options[i] = opt.(playwright.PageGotoOptions)
			}
		}
		if _, err = page.Goto(command.Value); err != nil {
			err = errors.Wrap(err, fmt.Sprintf("could not goto \"%s\"", command.Value))
		}
	default:
		return result, fmt.Errorf("cannot execute command type %d", ct)
	}
	return result, err
}

// String returns the name of the CommandType.
func (ct CommandType) String() string {
	switch ct {
	case Select:
		return "Select"
	case SelectAll:
		return "SelectAll"
	case Find:
		return "Find"
	case Type:
		return "Type"
	case Click:
		return "Click"
	case InnerText:
		return "InnerText"
	case Goto:
		return "Goto"
	default:
		return "<nil>"
	}
}

// Command represents a command that can be passed to the Browser.Flow method.
type Command struct {
	// Type is the type of the command, and will determine what is executed on the playwright.Page.
	Type CommandType
	// Value is the selector that will be used when Type is Select or SelectAll and the text that will be input when
	// Type is Type.
	Value string
	// Options will be asserted to the correct type on execution.
	Options []any
	// Wait indicates whether the command should wait for the query selector to become visible. Thus, it is only
	// applicable when Type is Select or SelectAll.
	Wait bool
	// Optional indicates that this command can fail, and it will be skipped.
	Optional bool
	// FindFunc is the function to execute on each element in the input elements when the Type is Find or FindInnerText.
	// When the Type is Find, then element can be asserted to playwright.ElementHandle, otherwise it will be a string.
	FindFunc func(element any) bool
}

// Execute wraps the Execute method for CommandType.
func (c *Command) Execute(page playwright.Page, previousResult *CommandResult) (result *CommandResult, err error) {
	if result, err = c.Type.Execute(page, c, previousResult); err != nil {
		if !c.Optional {
			return result, errors.Wrap(err, fmt.Sprintf("execution failed for %s", c.String()))
		} else {
			// When the Command is optional we will set the result to the previous result.
			result = previousResult
		}
	}
	return result, nil
}

// String returns the string representation of the Command. This returns a string that is similar to Go's representation
// of structs, but it also includes the field names. The Selector field will only be included if the Type is Type, Select,
// or SelectAll and the Wait field will only be included if the Type is Select. FindFunc will never be included.
func (c *Command) String() string {
	segments := make([]string, 1)
	segments[0] = fmt.Sprintf("Type: %s", c.Type.String())
	if int(c.Type) >= int(Select) && int(c.Type) < int(SelectAll) {
		segments = append(segments, fmt.Sprintf("Value: \"%s\"", c.Value))
		if int(c.Type) == int(Select) {
			segments = append(segments, fmt.Sprintf("Wait: %t", c.Wait))
		}
	}
	if len(c.Options) > 0 {
		segments = append(segments, fmt.Sprintf("Options: %v", c.Options))
	}
	segments = append(segments, fmt.Sprintf("Optional: %t", c.Optional))
	return fmt.Sprintf("{%s}", strings.Join(segments, ", "))
}

// Browser wraps the main instances needed for playwright.
type Browser struct {
	Playwright *playwright.Playwright
	Browser    playwright.Browser
	Pages      []playwright.Page
}

// NewBrowser creates a new playwright.Playwright, playwright.Browser, and a playwright.Page and wraps these up in a new
// Browser instance.
func NewBrowser(headless bool) (browser *Browser, err error) {
	browser = &Browser{}
	if browser.Playwright, err = playwright.Run(); err != nil {
		return browser, errors.Wrap(err, "browser could not be created as playwright could not be started")
	}
	if browser.Browser, err = browser.Playwright.Firefox.Launch(playwright.BrowserTypeLaunchOptions{
		Headless: &headless,
	}); err != nil {
		return browser, errors.Wrap(err, "browser could not be created as Firefox could not be launched")
	}
	jsEnabled := true
	if _, err = browser.Browser.NewContext(playwright.BrowserNewContextOptions{
		JavaScriptEnabled: &jsEnabled,
		UserAgent:         &UserAgent,
	}); err != nil {
		return browser, errors.Wrap(err, "browser could not be created as context could not be set")
	}
	browser.Pages = make([]playwright.Page, 0)
	if err = browser.NewPage(); err != nil {
		return browser, errors.Wrap(err, "browser could not be created")
	}
	return browser, nil
}

// NewPage creates a new page in the Browser.
func (b *Browser) NewPage() error {
	var err error
	var page playwright.Page
	if page, err = b.Browser.NewPage(playwright.BrowserNewContextOptions{
		UserAgent: &UserAgent,
	}); err != nil {
		return errors.Wrap(err, "page could not be created")
	}
	if err = page.SetExtraHTTPHeaders(map[string]string{
		"Accept":                    "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8",
		"Accept-Encoding":           "gzip, deflate, br",
		"Accept-Language":           "en-US,en;q=0.5",
		"Sec-Fetch-Dest":            "document",
		"Sec-Fetch-Mode":            "navigate",
		"Sec-Fetch-Site":            "none",
		"Sec-Fetch-User":            "?1",
		"Upgrade-Insecure-Requests": "1",
		"User-Agent":                UserAgent,
	}); err != nil {
		return errors.Wrap(err, "page could not be created because extra headers could not be set")
	}
	//page.SetDefaultTimeout(10000.0)
	b.Pages = append(b.Pages, page)
	return nil
}

// Quit the instance of the Browser. This will close the playwright.Browser instance and stop the playwright.Playwright
// instance.
func (b *Browser) Quit() (err error) {
	if err = b.Browser.Close(); err != nil {
		return errors.Wrap(err, "could not close the playwright.Browser instance")
	}
	if err = b.Playwright.Stop(); err != nil {
		return errors.Wrap(err, "could not stop the playwright.Playwright instance")
	}
	return nil
}

// Flow executes a series of Command, and returns a list of string outputs from the CommandResult.InnerTexts field.
func (b *Browser) Flow(pageNumber int, commands ...Command) ([]string, error) {
	var err error
	result := NewCommandResult()
	page := b.Pages[pageNumber]
	for i, command := range commands {
		if result, err = command.Execute(page, result); err != nil {
			return result.InnerTexts, errors.Wrap(err, fmt.Sprintf("flow failed on command %d", i))
		}
	}
	return result.InnerTexts, nil
}
