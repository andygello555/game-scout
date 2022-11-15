package steamcmd

import "fmt"

func ExampleSteamCMD_Flow() {
	var err error
	cmd := New(true)

	if err = cmd.Flow(
		NewCommandWithArgs(AppInfoPrint, 477160),
		NewCommandWithArgs(Quit),
	); err != nil {
		fmt.Printf("Could not execute flow: %s\n", err.Error())
	}
	fmt.Println(cmd.ParsedOutputs[0].(map[string]any)["common"].(map[string]any)["name"])
	// Output:
	// Human: Fall Flat
}
