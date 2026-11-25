package guestview

import "strings"

// About is the browser and the system a user agent names, in words: what
// the host is shown of the guest. It is what the browser says, which
// anybody can have it say.
func About(agent string) string {
	browser := ""
	for _, b := range []struct{ mark, name string }{
		// Each names those after it as well.
		{"Edg", "Edge"}, {"OPR/", "Opera"}, {"SamsungBrowser/", "Samsung Internet"}, {"Firefox/", "Firefox"}, {"FxiOS/", "Firefox"},
		{"CriOS/", "Chrome"}, {"Chrome/", "Chrome"}, {"Safari/", "Safari"},
	} {
		if strings.Contains(agent, b.mark) {
			browser = b.name
			break
		}
	}
	system := ""
	for _, s := range []struct{ mark, name string }{
		{"Android", "Android"}, {"iPhone", "iPhone"}, {"iPad", "iPad"}, {"Windows", "Windows"}, {"CrOS", "ChromeOS"},
		{"Mac OS X", "macOS"}, {"Linux", "Linux"},
	} {
		if strings.Contains(agent, s.mark) {
			system = s.name
			break
		}
	}
	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case browser != "":
		return browser
	}
	return system
}
