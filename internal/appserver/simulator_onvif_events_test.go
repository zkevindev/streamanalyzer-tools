package appserver

import (
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"streamanalyzer/internal/simulator/onvif"
)

const simulatorEventNamespace = "http://www.onvif.org/ver10/events/wsdl"
const simulatorNotificationNamespace = "http://docs.oasis-open.org/wsn/b-2"

func TestONVIFPullPointMotionAndLease(t *testing.T) {
	server := testDiagnosticServer(t, t.TempDir())
	start := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, "POST", onvifAPI+"/start", http.StatusCreated, []byte(`{"clock_offset_seconds":30}`)))
	client := &http.Client{Timeout: 2 * time.Second}
	soap := func(addr, ns, operation, body string, status int) string {
		t.Helper()
		return queryONVIF(t, client, addr, ns, operation, body, status)
	}
	for _, operation := range []string{"GetCapabilities", "GetServices"} {
		if body := soap(start.DeviceURL, simulatorDeviceNamespace, operation, "", 200); !strings.Contains(body, start.EventURL) {
			t.Fatalf("%s did not advertise the event endpoint: %s", operation, body)
		}
	}
	if body := soap(start.EventURL, simulatorEventNamespace, "GetEventProperties", "", 200); !strings.Contains(body, "CellMotionDetector") || !strings.Contains(body, "MessageContentSchemaLocation") {
		t.Fatal(body)
	}
	subscribe := func(lease string) string {
		t.Helper()
		body := soap(start.EventURL, simulatorEventNamespace, "CreatePullPointSubscription",
			`<o:Filter><wsnt:TopicExpression xmlns:wsnt="http://docs.oasis-open.org/wsn/b-2" Dialect="http://docs.oasis-open.org/wsn/t-1/TopicExpression/Concrete">tns1:RuleEngine/CellMotionDetector/Motion</wsnt:TopicExpression></o:Filter>`+
				"<o:InitialTerminationTime>"+lease+"</o:InitialTerminationTime>", 200)
		address := onvifEventText(body, "wsa:Address")
		current, err := time.Parse(time.RFC3339Nano, onvifEventText(body, "wsnt:CurrentTime"))
		if address == "" || err != nil || time.Until(current) < 28*time.Second {
			t.Fatalf("invalid event subscription: %s", body)
		}
		return address
	}
	first := subscribe("PT2S")
	second := subscribe("PT3S")
	if first == second {
		t.Fatal("expected independent subscription addresses")
	}
	pull := func(addr, timeout string, status int) string {
		t.Helper()
		return soap(addr, simulatorEventNamespace, "PullMessages", "<o:Timeout>"+timeout+"</o:Timeout><o:MessageLimit>1</o:MessageLimit>", status)
	}
	for _, addr := range []string{first, second} {
		body := pull(addr, "PT0S", 200)
		if !strings.Contains(body, `PropertyOperation="Initialized"`) || !strings.Contains(body, `Name="IsMotion" Value="false"`) {
			t.Fatal(body)
		}
	}
	before := time.Now()
	body := pull(first, "PT0.1S", 200)
	if strings.Contains(body, "NotificationMessage") || time.Since(before) < 90*time.Millisecond {
		t.Fatalf("empty poll returned early or delivered an event: %s", body)
	}
	for _, value := range []string{"true", "false"} {
		end := make(chan string, 1)
		go func() {
			req := `<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:tev="http://www.onvif.org/ver10/events/wsdl"><s:Body><tev:PullMessages><tev:Timeout>PT1S</tev:Timeout><tev:MessageLimit>1</tev:MessageLimit></tev:PullMessages></s:Body></s:Envelope>`
			res, err := client.Post(first, "application/soap+xml", strings.NewReader(req))
			if err != nil {
				end <- err.Error()
				return
			}
			defer res.Body.Close()
			var text strings.Builder
			_, _ = io.Copy(&text, res.Body)
			end <- text.String()
		}()
		deadline := time.Now().Add(time.Second)
		previous := uint64(3)
		if value == "false" {
			previous = 5
		}
		for {
			status := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, "GET", onvifAPI, http.StatusOK, nil))
			if status.Events.PullRequests > previous {
				break
			}
			if time.Now().After(deadline) {
				t.Fatal("PullMessages was not observed")
			}
			time.Sleep(10 * time.Millisecond)
		}
		diagnosticRequest(t, server, "POST", onvifAPI+"/motion", http.StatusOK, []byte(`{"active":`+value+`}`))
		for _, notification := range []string{<-end, pull(second, "PT0S", 200)} {
			if !strings.Contains(notification, `PropertyOperation="Changed"`) || !strings.Contains(notification, `Name="IsMotion" Value="`+value+`"`) {
				t.Fatalf("missing transition %s: %s", value, notification)
			}
		}
	}
	renewed := soap(first, simulatorNotificationNamespace, "Renew", "<o:TerminationTime>PT0.25S</o:TerminationTime>", 200)
	expiry, err := time.Parse(time.RFC3339Nano, onvifEventText(renewed, "wsnt:TerminationTime"))
	if err != nil || expiry.Sub(time.Now()) > 31*time.Second {
		t.Fatalf("wrong renewed lease: %s", renewed)
	}
	soap(second, simulatorNotificationNamespace, "Unsubscribe", "", 200)
	body = pull(second, "PT0S", 400)
	if !strings.Contains(body, "ResourceUnknownFault") {
		t.Fatal(body)
	}
	time.Sleep(270 * time.Millisecond)
	body = pull(first, "PT0S", 400)
	if !strings.Contains(body, "ResourceUnknownFault") {
		t.Fatal(body)
	}
	status := decodeDiagnostic[onvif.Status](t, diagnosticRequest(t, server, "GET", onvifAPI, http.StatusOK, nil))
	if status.Events.Subscriptions != 0 || status.Events.Delivered != 6 || status.Events.Changes != 2 {
		t.Fatalf("incorrect event evidence: %+v", status.Events)
	}
	if address := subscribe("PT2S"); address == first || address == second {
		t.Fatal("subscription was reused after expiry")
	}
}

func onvifEventText(body, tag string) string {
	_, value, _ := strings.Cut(body, "<"+tag+">")
	text, _, _ := strings.Cut(value, "</"+tag+">")
	return text
}
