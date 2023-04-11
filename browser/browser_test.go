package browser

import (
	"fmt"
	"github.com/playwright-community/playwright-go"
)

func ExampleNewBrowser() {
	browser, err := NewBrowser(true)
	if err != nil {
		fmt.Println("could not create Browser:", err)
	}
	if _, err = browser.Pages[0].Goto("https://github.com/playwright-community/playwright-go"); err != nil {
		fmt.Println("could not goto:", err)
	}
	var entry playwright.ElementHandle
	if entry, err = browser.Pages[0].QuerySelector("title"); err != nil {
		fmt.Println("could not get title:", err)
	}
	fmt.Println(entry.InnerText())
	if err = browser.Quit(); err != nil {
		fmt.Println("could not quit browser")
	}
	fmt.Println("Done!")
	// Output:
	// GitHub - playwright-community/playwright-go: Playwright for Go a browser automation library to control Chromium, Firefox and WebKit with a single API. <nil>
	// Done!
}
