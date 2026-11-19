package onvif

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/google/uuid"
)

var ErrEventsUnavailable = errors.New("ONVIF 模拟器未启动")

func eventName(ns, local string) xml.Name { return xml.Name{Space: ns, Local: local} }

const (
	eventNS              = "http://www.onvif.org/ver10/events/wsdl"
	eventPath            = "/onvif/event_service"
	pullPath             = "/onvif/events/"
	notificationNS       = "http://docs.oasis-open.org/wsn/b-2"
	topicNS              = "http://www.onvif.org/ver10/topics"
	topicDialect         = "http://www.onvif.org/ver10/tev/topicExpression/ConcreteSet"
	concreteTopicDialect = "http://docs.oasis-open.org/wsn/t-1/TopicExpression/Concrete"
	motionTopic          = "RuleEngine/CellMotionDetector/Motion"
	eventCapabilities    = `<tev:Capabilities WSSubscriptionPolicySupport="false" WSPullPointSupport="true" WSPausableSubscriptionManagerInterfaceSupport="false" MaxPullPoints="16"/>`
	maxPullWait          = 25 * time.Second
	maxPullPoints        = 16
	maxQueuedEvents      = 32
	defaultEventLease    = time.Minute
	maxEventLease        = time.Hour
)

type EventStatus struct {
	Motion        bool   `json:"motion"`
	Subscriptions int    `json:"subscriptions"`
	PullRequests  uint64 `json:"pull_requests"`
	Changes       uint64 `json:"changes"`
	Delivered     uint64 `json:"delivered"`
	Dropped       uint64 `json:"dropped"`
}

type motionEvent struct {
	at        time.Time
	active    bool
	operation string
}

type pullPoint struct {
	expires time.Time
	events  []motionEvent
	update  chan struct{}
	pulling bool
}

func (p *pullPoint) wake() {
	close(p.update)
	p.update = make(chan struct{})
}

type eventBroker struct {
	mu     sync.Mutex
	status EventStatus
	points map[string]*pullPoint
}

func (b *eventBroker) prune(now time.Time) {
	for id, point := range b.points {
		if !now.Before(point.expires) {
			delete(b.points, id)
			point.wake()
		}
	}
}

func (b *eventBroker) enqueue(point *pullPoint, event motionEvent) {
	if len(point.events) == maxQueuedEvents {
		copy(point.events, point.events[1:])
		point.events = point.events[:maxQueuedEvents-1]
		b.status.Dropped++
	}
	point.events = append(point.events, event)
	point.wake()
}

func (b *eventBroker) snapshot() EventStatus {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(time.Now())
	status := b.status
	status.Subscriptions = len(b.points)
	return status
}

func (m *Manager) SetMotion(active bool) (Status, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.camera == nil {
		return Status{}, ErrEventsUnavailable
	}
	c := m.camera
	b := &c.events
	b.mu.Lock()
	b.prune(time.Now())
	if b.status.Motion != active {
		b.status.Motion = active
		b.status.Changes++
		event := motionEvent{at: c.clock(), active: active, operation: "Changed"}
		for _, point := range b.points {
			b.enqueue(point, event)
		}
	}
	b.mu.Unlock()
	return c.snapshot(), nil
}

func (c *camera) eventReply(r *http.Request, op operation) (string, *soapFault) {
	id := r.PathValue("subscription")
	if id == "" && op.XMLName.Space == eventNS {
		switch op.XMLName.Local {
		case "GetServiceCapabilities":
			return eventCapabilities, nil
		case "GetEventProperties":
			return eventProperties(), nil
		case "CreatePullPointSubscription":
			return c.createPullPoint(op)
		}
		return "", unsupportedAction()
	}
	switch op.XMLName {
	case eventName(eventNS, "PullMessages"):
		return c.pullMessages(r.Context(), id, op)
	case eventName(eventNS, "SetSynchronizationPoint"):
		return c.updatePullPoint(id, op)
	case eventName(notificationNS, "Renew"), eventName(notificationNS, "Unsubscribe"):
		return c.updatePullPoint(id, op)
	default:
		return "", unsupportedAction()
	}
}

func (c *camera) createPullPoint(op operation) (string, *soapFault) {
	now := time.Now()
	lease, ok := eventLease(op.InitialTerminationTime, c.deviceTime(now))
	if !ok {
		return "", invalidToken("Invalid InitialTerminationTime")
	}
	if op.Filter != nil && !op.Filter.supported() {
		return "", &soapFault{code: "wsnt:TopicExpressionDialectUnknownFault", message: "Only the simulator motion topic is supported"}
	}
	b := &c.events
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(now)
	if len(b.points) >= maxPullPoints {
		return "", &soapFault{code: "MaxPullPoints", message: "Maximum PullPoint subscriptions reached"}
	}
	if b.points == nil {
		b.points = make(map[string]*pullPoint)
	}
	id := uuid.NewString()
	point := &pullPoint{expires: now.Add(lease), update: make(chan struct{})}
	b.enqueue(point, motionEvent{at: c.deviceTime(now), active: b.status.Motion, operation: "Initialized"})
	b.points[id] = point
	address := strings.TrimSuffix(c.status.EventURL, eventPath) + pullPath + id
	return "<tev:SubscriptionReference><wsa:Address>" + address + "</wsa:Address></tev:SubscriptionReference>" +
		c.eventTimes("wsnt", now, point.expires), nil
}

func (c *camera) updatePullPoint(id string, op operation) (string, *soapFault) {
	now := time.Now()
	b := &c.events
	b.mu.Lock()
	defer b.mu.Unlock()
	b.prune(now)
	point := b.points[id]
	if point == nil {
		return "", unknownPullPoint()
	}
	switch op.XMLName.Local {
	case "Renew":
		lease, ok := eventLease(op.TerminationTime, c.deviceTime(now))
		if !ok {
			return "", invalidToken("Invalid TerminationTime")
		}
		point.expires = now.Add(lease)
		point.wake()
		return c.eventTime("wsnt:TerminationTime", point.expires) + c.eventTime("wsnt:CurrentTime", now), nil
	case "Unsubscribe":
		delete(b.points, id)
		point.wake()
	case "SetSynchronizationPoint":
		b.enqueue(point, motionEvent{at: c.deviceTime(now), active: b.status.Motion, operation: "Initialized"})
	}
	return "", nil
}

func (c *camera) pullMessages(ctx context.Context, id string, op operation) (string, *soapFault) {
	wait, ok := eventDuration(op.Timeout)
	if !ok {
		return "", invalidToken("Invalid PullMessages Timeout")
	}
	limit, err := strconv.Atoi(strings.TrimSpace(op.MessageLimit))
	if err != nil || limit <= 0 {
		return "", invalidToken("Invalid PullMessages MessageLimit")
	}
	limit = min(limit, maxQueuedEvents)
	deadline := time.Now().Add(min(wait, maxPullWait))
	b := &c.events
	b.mu.Lock()
	b.prune(time.Now())
	point := b.points[id]
	if point == nil {
		b.mu.Unlock()
		return "", unknownPullPoint()
	}
	if point.pulling {
		b.mu.Unlock()
		return "", invalidToken("A PullMessages request is already pending")
	}
	point.pulling = true
	b.status.PullRequests++
	defer func() {
		point.pulling = false
		b.mu.Unlock()
	}()
	for {
		now := time.Now()
		b.prune(now)
		if b.points[id] != point {
			return "", unknownPullPoint()
		}
		if len(point.events) > 0 || !now.Before(deadline) {
			n := min(limit, len(point.events))
			var body strings.Builder
			body.WriteString(c.eventTimes("tev", now, point.expires))
			for _, event := range point.events[:n] {
				body.WriteString(event.xml())
			}
			point.events = point.events[n:]
			b.status.Delivered += uint64(n)
			return body.String(), nil
		}
		update := point.update
		timer := time.NewTimer(min(deadline.Sub(now), point.expires.Sub(now)))
		b.mu.Unlock()
		select {
		case <-update:
		case <-timer.C:
		case <-ctx.Done():
		}
		timer.Stop()
		b.mu.Lock()
		if ctx.Err() != nil {
			return "", nil
		}
	}
}

func unknownPullPoint() *soapFault {
	return &soapFault{code: "wsrf:ResourceUnknownFault", message: "PullPoint subscription expired or does not exist"}
}

func (c *camera) deviceTime(at time.Time) time.Time {
	return at.UTC().Add(time.Duration(c.status.ClockOffset) * time.Second)
}

func (c *camera) eventTimes(prefix string, now, expiry time.Time) string {
	return c.eventTime(prefix+":CurrentTime", now) + c.eventTime(prefix+":TerminationTime", expiry)
}

func (c *camera) eventTime(element string, timestamp time.Time) string {
	return fmt.Sprintf("<%[1]s>%[2]s</%[1]s>", element, c.deviceTime(timestamp).Format(time.RFC3339Nano))
}

func (e motionEvent) xml() string {
	return fmt.Sprintf(`<wsnt:NotificationMessage><wsnt:Topic Dialect="%s">tns1:%s</wsnt:Topic><wsnt:Message>
<tt:Message UtcTime="%s" PropertyOperation="%s"><tt:Source>
<tt:SimpleItem Name="VideoSourceConfigurationToken" Value="source-config-1"/><tt:SimpleItem Name="VideoAnalyticsConfigurationToken" Value="analytics-1"/><tt:SimpleItem Name="Rule" Value="Motion"/>
</tt:Source><tt:Data><tt:SimpleItem Name="IsMotion" Value="%t"/></tt:Data></tt:Message></wsnt:Message></wsnt:NotificationMessage>`,
		concreteTopicDialect, motionTopic, e.at.Format(time.RFC3339Nano), e.operation, e.active)
}

func eventProperties() string {
	return `<tev:TopicNamespaceLocation>http://www.onvif.org/ver10/topics/topicns.xml</tev:TopicNamespaceLocation><wsnt:FixedTopicSet>true</wsnt:FixedTopicSet>
<wstop:TopicSet><tns1:RuleEngine><tns1:CellMotionDetector><tns1:Motion wstop:topic="true"><tt:MessageDescription IsProperty="true">
<tt:Source><tt:SimpleItemDescription Name="VideoSourceConfigurationToken" Type="tt:ReferenceToken"/><tt:SimpleItemDescription Name="VideoAnalyticsConfigurationToken" Type="tt:ReferenceToken"/><tt:SimpleItemDescription Name="Rule" Type="xs:string"/></tt:Source>
<tt:Data><tt:SimpleItemDescription Name="IsMotion" Type="xs:boolean"/></tt:Data></tt:MessageDescription></tns1:Motion></tns1:CellMotionDetector></tns1:RuleEngine></wstop:TopicSet>
<wsnt:TopicExpressionDialect>` + concreteTopicDialect + `</wsnt:TopicExpressionDialect><wsnt:TopicExpressionDialect>` + topicDialect + `</wsnt:TopicExpressionDialect>
<tev:MessageContentFilterDialect/><tev:MessageContentSchemaLocation>http://www.onvif.org/ver10/schema/onvif.xsd</tev:MessageContentSchemaLocation>`
}

type eventFilter struct {
	Topics  []elementText `xml:"TopicExpression"`
	Content *elementText  `xml:"MessageContent"`
}

func (f *eventFilter) supported() bool {
	if f.Content != nil {
		return false
	}
	for _, expression := range f.Topics {
		for _, attr := range expression.Attrs {
			if attr.Name.Local == "Dialect" && attr.Value != topicDialect && attr.Value != concreteTopicDialect {
				return false
			}
		}
		topic := strings.TrimSpace(expression.Text)
		if _, local, ok := strings.Cut(topic, ":"); ok {
			topic = local
		}
		if topic != motionTopic {
			return false
		}
	}
	return true
}

var eventDurationPattern = regexp.MustCompile(`^PT(?:(\d+)H)?(?:(\d+)M)?(?:(\d+(?:\.\d+)?)S)?$`)

func eventDuration(raw string) (time.Duration, bool) {
	parts := eventDurationPattern.FindStringSubmatch(strings.TrimSpace(raw))
	if len(parts) == 0 || parts[1] == "" && parts[2] == "" && parts[3] == "" {
		return 0, false
	}
	hours, _ := strconv.ParseFloat(parts[1], 64)
	minutes, _ := strconv.ParseFloat(parts[2], 64)
	seconds, _ := strconv.ParseFloat(parts[3], 64)
	total := hours*3600 + minutes*60 + seconds
	if math.IsInf(total, 0) || total > 24*60*60 {
		return 0, false
	}
	return time.Duration(total * float64(time.Second)), true
}

func eventLease(raw string, now time.Time) (time.Duration, bool) {
	if strings.TrimSpace(raw) == "" {
		return defaultEventLease, true
	}
	d, ok := eventDuration(raw)
	if !ok {
		absolute, err := time.Parse(time.RFC3339Nano, strings.TrimSpace(raw))
		if err != nil {
			return 0, false
		}
		d = absolute.Sub(now)
	}
	return min(d, maxEventLease), d > 0
}
