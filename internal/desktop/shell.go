package desktop

import (
	_ "embed"
	"html/template"
	"strings"
)

//go:embed shell.html
var shellDocument string

//go:embed update.js
var updateScript string

var shellTemplate = template.Must(template.New("shell").Parse(shellDocument))

func shellHTML(serverURL, settingsToken, updateToken string) string {
	var page strings.Builder
	if err := shellTemplate.Execute(&page, struct {
		WorkbenchURL  string
		SettingsToken string
		UpdateToken   string
	}{
		WorkbenchURL:  serverURL + "/diagnostics",
		SettingsToken: settingsToken,
		UpdateToken:   updateToken,
	}); err != nil {
		return errorHTML("无法加载桌面工作台")
	}
	return page.String()
}
