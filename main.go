package main

import (
	"fmt"
	"log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/brycensranch/go-aptabase/pkg/aptabase/v1"
	"github.com/brycensranch/go-aptabase/pkg/osinfo/v1"

	"github.com/getsentry/sentry-go"
	"github.com/jwijenbergh/puregotk/v4/gio"
	"github.com/jwijenbergh/puregotk/v4/gtk"
)

var aptabaseClient *aptabase.Client // Package-level variable

func chooseNonEmpty(first, second string) string {
	if first != "" {
		return first
	}
	return second
}

func getOSRelease() string {
	osName, osVersion := osinfo.GetOSInfo()
	return fmt.Sprintf("%s %s", osName, osVersion)
}

func createEvent(eventName string, eventData map[string]interface{}) aptabase.EventData {
	event := aptabase.EventData{
		EventName: eventName,
		Props:     eventData,
	}
	aptabaseClient.TrackEvent(event)
	return event
}

func main() {
	version := "0.0.0-PUREGO"
	isPackaged := "false"
	packageFormat := "native"

	fmt.Println("Starting Rokon. Now with more telemetry!")
	err := sentry.Init(sentry.ClientOptions{
		Dsn:                "https://04484623ba4aa6cbb830e852178e9358@o4504136997928960.ingest.us.sentry.io/4507991443439616",
		Release:            version,
		EnableTracing:      true,
		AttachStacktrace:   true,
		TracesSampleRate:   1.0,
		ProfilesSampleRate: 1.0,
		// Only enable Debug if the environment variable TRANSPARENT_TELEMETRY is set
		Debug: os.Getenv("TRANSPARENT_TELEMETRY") != "",
		BeforeSend: func(event *sentry.Event, hint *sentry.EventHint) *sentry.Event {
			// TRANSPARENT_TELEMETRY is set, so we can log the event and what data it's sending
			if os.Getenv("TRANSPARENT_TELEMETRY") != "" {
				log.Printf("Sending event: %s\n", chooseNonEmpty(event.Type, event.Message))
				log.Printf("Event ID: %v\n", chooseNonEmpty(hint.EventID, string(event.EventID)))
				log.Printf("Event data: %v\n", event)
			}
			return event
		},
		// Integrations: [
		// 	sentry.Integration
		// ],
	})
	if err != nil {
		log.Fatalf("sentry.Init: %s", err)
	}
	aptabaseClient = aptabase.NewClient("A-US-0332858461", version, uint64(133), true, "")
	app := gtk.NewApplication("io.github.brycensranch.Rokon", gio.GApplicationFlagsNoneValue)
	// cleanup, no finalizers are used in this library
	defer app.Unref()
	switch runtime.GOOS {
	case "linux":
		release := getOSRelease()
		arch := runtime.GOARCH
		desktop := os.Getenv("XDG_CURRENT_DESKTOP")
		sessionType := os.Getenv("XDG_SESSION_TYPE")

		kdeSessionVersion := ""
		if desktop == "KDE" {
			kdeSessionVersion = os.Getenv("KDE_SESSION_VERSION")
		}

		log.Printf("Running on Linux %s %s with %s %s %s and %s\n",
			release, arch, desktop, os.Getenv("DESKTOP_SESSION"), kdeSessionVersion, sessionType)

		createEvent("linux_run", map[string]interface{}{
			"arch":        arch,
			"desktop":     desktop,
			"sessionType": sessionType,
		})

		container := os.Getenv("container")
		if container != "" && container == "flatpak" {
			log.Println("Running from a Flatpak")
			createEvent("flatpak_run", map[string]interface{}{
				"flatpak":        container,
				"flatpakVersion": version, // Replace with your app version logic
			})
		} else if snap := os.Getenv("SNAP"); snap != "" {
			log.Println("Running from a Snap")
			createEvent("snap_run", map[string]interface{}{
				"snap":        snap,
				"snapVersion": version, // Replace with your app version logic
			})
		} else if appImage := os.Getenv("APPIMAGE"); appImage != "" {
			log.Println("Running from an AppImage")
			firejail := isRunningWithFirejail()

			if firejail {
				log.Println("Running from an AppImage with firejail")
				// Adjust telemetry or other settings as needed.
			}

			createEvent("appimage_run", map[string]interface{}{
				"appimage":           appImage,
				"appimageVersion":    version, // Replace with your app version logic
				"firejail":           firejail,
				"desktopIntegration": os.Getenv("DESKTOPINTEGRATION"),
			})
		} else if isPackaged == "true" {
			log.Println("Running from a native package")
			createEvent("native_run", map[string]interface{}{
				"nativeVersion": version, // Replace with your app version logic
				"path":          os.Args[0],
			})
		}
	case "windows":
		release := getOSRelease()
		arch := runtime.GOARCH
		log.Printf("Running on Windows %s %s\n",
			release, arch)

		if portable := packageFormat; portable == "portable" {
			log.Println("Running from a portable executable")
		}

		createEvent("windows_run", map[string]interface{}{
			"arch":          arch,
			"version":       version, // Replace with your app version logic
			"packageFormat": packageFormat,
		})
	case "darwin":
		release := getOSRelease()
		arch := runtime.GOARCH
		log.Printf("Running on macOS %s %s with %s\n",
			release, arch, os.Getenv("XPC_FLAGS"))

		createEvent("macos_run", map[string]interface{}{
			"arch":    arch,
			"mas":     os.Getenv("MAS"),
			"version": version, // Replace with your app version logic
			"path":    os.Args[0],
		})
	default:
		log.Printf("Unsupported telemetry platform: %s %s %s. However, the application will continue.\n",
			runtime.GOOS, getOSRelease(), runtime.GOARCH)
		createEvent("unsupported_platform", map[string]interface{}{
			"platform": runtime.GOOS,
			"arch":     runtime.GOARCH,
			"version":  version, // Replace with your app version logic
			"path":     os.Args[0],
		})
	}
	// functions with callback arguments take function pointers
	// this is for internal re-use of callbacks
	actcb := func(_ gio.Application) {
		activate(app)
	}
	app.ConnectActivate(&actcb)
	// Flush buffered events before the program terminates.
	// Set the timeout to the maximum duration the program can afford to wait.
	defer sentry.Flush(2 * time.Second)
	aptabaseClient.Quit = true
	aptabaseClient.Stop()
	if code := app.Run(len(os.Args), os.Args); code > 0 {
		os.Exit(code)
	}
}

func applicationInfo(app *gtk.Application) string {
	qualifier := func() string {
		switch {
		case os.Getenv("SNAP") != "":
			return " (Snap)"
		case os.Getenv("FLATPAK") != "":
			return " (Flatpak)"
		case os.Getenv("APPIMAGE") != "":
			return " (AppImage)"
		case os.Getenv("CONTAINER") != "":
			return " (Container)"
		default:
			return ""
		}
	}()
	return fmt.Sprintf("Rokon %s%s", "0.0.0-PUREGO", qualifier)
}

func activate(app *gtk.Application) {
	window := gtk.NewApplicationWindow(app)
	window.SetTitle("Rokon: Control your Roku from your desktop")
	window.SetChild(&gtk.NewLabel("Hello from Go!").Widget)
	aboutWindow := gtk.NewAboutDialog()
	aboutWindow.SetProgramName(applicationInfo(app))
	aboutWindow.SetVersion("0.0.0-PUREGO")
	aboutWindow.SetComments("Control your Roku TV from your desktop")
	aboutWindow.SetWebsite("https://github.com/BrycensRanch/Rokon")
	aboutWindow.SetWebsiteLabel("GitHub")
	aboutWindow.SetSystemInformation(
		("GTK: " + strconv.Itoa(int(gtk.GetMajorVersion())) + "." + strconv.Itoa(int(gtk.GetMinorVersion())) + "." + strconv.Itoa(int(gtk.GetMicroVersion()))))
	aboutWindow.SetCopyright("2024 Brycen G and contributors, but mostly Brycen")
	aboutWindow.SetWrapLicense(true)
	aboutWindow.SetModal(true)
	aboutWindow.SetDestroyWithParent(true)

	switch {
	case os.Getenv("SNAP") != "":
		image := gtk.NewImageFromFile(os.Getenv("SNAP") + "/meta/gui/icon.png")
		if image != nil {
			logo := image.GetPaintable()
			if logo != nil {
				aboutWindow.SetLogo(logo)
			}
		}
	case os.Getenv("FLATPAK") != "":
		image := gtk.NewImageFromFile("/app/share/icons/hicolor/256x256/apps/io.github.brycensranch.Rokon.png")
		if image != nil {
			logo := image.GetPaintable()
			if logo != nil {
				aboutWindow.SetLogo(logo)
			}
		}
	default:
		// Assume native packaging
		aboutWindow.SetLogoIconName("io.github.brycensranch.Rokon")
		sentry.CaptureMessage("Something went wrong " + applicationInfo(app))

		if os.Getenv("CONTAINER") != "" {
			log.Println("Running in a container, the logo icon may not be displayed due to wrong path")
		}
	}
	// Capture an error and send it to Sentry
	// err := fmt.Errorf("something went wrong!")
	// sentry.CaptureException(err)
	// aboutWindow.SetAuthors([]string{"Brycen G. (BrycensRanch)"})
	aboutWindow.SetLicenseType(gtk.LicenseAgpl30Value)
	// window.SetChild(&aboutWindow.Window)

	aboutWindow.Present()
	const windowSize = 400
	window.SetDefaultSize(windowSize, windowSize)
	// set window position to center

	window.SetVisible(true)
}

func isRunningWithFirejail() bool {
	appImage := os.Getenv("APPIMAGE")
	appDir := os.Getenv("APPDIR")
	return (appImage != "" && (appImage[len(appImage)-10:] == "/run/firejail" || contains(appImage, "/run/firejail"))) ||
		(appDir != "" && contains(appDir, "/run/firejail"))
}

// Helper function to check if a string contains a substring.
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}
