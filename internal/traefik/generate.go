package traefik

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/ku9nov/docker-compose-ztd-plugin/internal/compose"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/configio"
	"github.com/ku9nov/docker-compose-ztd-plugin/internal/types"
)

type Generator struct {
	compose compose.Adapter
	docker  labelReader
}

type labelReader interface {
	Labels(ctx context.Context, containerID string) (map[string]string, error)
}

func NewGenerator(composeAdapter compose.Adapter, dockerClient labelReader) *Generator {
	return &Generator{
		compose: composeAdapter,
		docker:  dockerClient,
	}
}

func (g *Generator) Generate(ctx context.Context, composeFiles []string, envFiles []string, outputPath string) error {
	enabledServices, err := collectTraefikEnabledServices(composeFiles)
	if err != nil {
		return err
	}
	if len(enabledServices) == 0 {
		return fmt.Errorf("no services with label traefik.enable=true were found")
	}

	serviceEndpoints := map[string][]string{}
	for _, svc := range enabledServices {
		ids, err := g.compose.PsQuiet(ctx, composeFiles, envFiles, svc)
		if err != nil {
			return err
		}
		for _, id := range ids {
			serviceEndpoints[svc] = append(serviceEndpoints[svc], shortID(id))
		}
	}

	allContainerIDs, err := g.compose.PsQuiet(ctx, composeFiles, envFiles, "")
	if err != nil {
		return err
	}

	cfg := types.DynamicConfig{
		HTTP: &types.HTTPConfig{
			Routers:  map[string]types.HTTPRouter{},
			Services: map[string]types.HTTPService{},
		},
		TCP: &types.TCPConfig{
			Routers:  map[string]types.TCPRouter{},
			Services: map[string]types.TCPService{},
		},
		UDP: &types.UDPConfig{
			Routers:  map[string]types.UDPRouter{},
			Services: map[string]types.UDPService{},
		},
	}
	processedServices := map[string]struct{}{}

	for _, id := range allContainerIDs {
		labels, err := g.docker.Labels(ctx, id)
		if err != nil {
			return err
		}
		// Normalize label keys to lowercase so lookups are case-insensitive,
		// mirroring Traefik's docker provider. Users can write labels in the
		// camelCase form shown in the official docs (ruleSyntax, serversTransport,
		// passHostHeader, httpOnly, sameSite, maxAge, followRedirects, ...) and
		// still match.
		labels = normalizeLabelKeys(labels)

		serviceName := labels["com.docker.compose.service"]
		if serviceName == "" {
			continue
		}

		endpoints, ok := serviceEndpoints[serviceName]
		if !ok || len(endpoints) == 0 {
			continue
		}
		if _, seen := processedServices[serviceName]; seen {
			continue
		}
		processedServices[serviceName] = struct{}{}

		// ── HTTP Routers (generic) ─────────────────────────────────────────────
		// Router names are discovered from label keys, so one compose service
		// can declare multiple routers under arbitrary names (e.g. "catchall"),
		// not just a router matching the compose service name.

		for _, rtrName := range httpRouterNames(labels) {
			routerRule := labels["traefik.http.routers."+rtrName+".rule"]
			if routerRule == "" {
				continue
			}
			routerSvc := labels["traefik.http.routers."+rtrName+".service"]
			if routerSvc == "" {
				routerSvc = serviceName
			}
			router := types.HTTPRouter{
				Rule:    routerRule,
				Service: routerSvc,
			}
			if eps := splitEntryPoints(labels["traefik.http.routers."+rtrName+".entrypoints"]); len(eps) > 0 {
				router.EntryPoints = eps
			}
			if mws := splitEntryPoints(labels["traefik.http.routers."+rtrName+".middlewares"]); len(mws) > 0 {
				router.Middlewares = mws
			}
			if pri := labels["traefik.http.routers."+rtrName+".priority"]; pri != "" {
				if n, err := strconv.Atoi(pri); err == nil {
					router.Priority = n
				}
			}
			if rs := labels["traefik.http.routers."+rtrName+".rulesyntax"]; rs != "" {
				router.RuleSyntax = rs
			}
			router.TLS = extractHTTPRouterTLS(labels, rtrName)
			cfg.HTTP.Routers[rtrName] = router
		}

		// ── HTTP Service ───────────────────────────────────────────────────────

		httpPort := labels["traefik.http.services."+serviceName+".loadbalancer.server.port"]
		if httpPort == "" {
			httpPort = "80"
		}
		httpScheme := labels["traefik.http.services."+serviceName+".loadbalancer.server.scheme"]
		if httpScheme == "" {
			httpScheme = "http"
		}

		httpWeightStr := labels["traefik.http.services."+serviceName+".loadbalancer.server.weight"]

		httpServers := make([]types.HTTPServer, 0, len(endpoints))
		for _, endpoint := range endpoints {
			srv := types.HTTPServer{URL: httpScheme + "://" + endpoint + ":" + httpPort}
			if httpWeightStr != "" {
				if n, err := strconv.Atoi(httpWeightStr); err == nil {
					srv.Weight = &n
				}
			}
			httpServers = append(httpServers, srv)
		}

		httpLB := types.HTTPLoadBalancer{Servers: httpServers}

		if pph := labels["traefik.http.services."+serviceName+".loadbalancer.passhostheader"]; pph != "" {
			b := pph == "true"
			httpLB.PassHostHeader = &b
		}
		if fi := labels["traefik.http.services."+serviceName+".loadbalancer.responseforwarding.flushinterval"]; fi != "" {
			httpLB.ResponseForwarding = &types.ResponseForwarding{FlushInterval: fi}
		}

		httpService := types.HTTPService{LoadBalancer: httpLB}
		if hc := extractHealthCheck(labels, serviceName); hc != nil {
			httpService.LoadBalancer.HealthCheck = hc
		}
		if sticky := extractSticky(labels, serviceName); sticky != nil {
			httpService.LoadBalancer.Sticky = sticky
		}
		if strategy := labels["traefik.http.services."+serviceName+".loadbalancer.strategy"]; strategy != "" {
			httpService.LoadBalancer.Strategy = strategy
		}
		if st := labels["traefik.http.services."+serviceName+".loadbalancer.serverstransport"]; st != "" {
			httpService.LoadBalancer.ServersTransport = st
		}
		cfg.HTTP.Services[serviceName] = httpService

		// ── HTTP Middlewares (generic) ─────────────────────────────────────────

		for mwName, mwConfig := range extractMiddlewares(labels) {
			if cfg.HTTP.Middlewares == nil {
				cfg.HTTP.Middlewares = map[string]map[string]interface{}{}
			}
			cfg.HTTP.Middlewares[mwName] = mwConfig
		}

		// ── TCP ────────────────────────────────────────────────────────────────

		for _, tcpName := range tcpRouterNames(labels) {
			tcpRule := labels["traefik.tcp.routers."+tcpName+".rule"]
			tcpServiceName := labels["traefik.tcp.routers."+tcpName+".service"]
			if tcpServiceName == "" {
				tcpServiceName = tcpName
			}
			tcpPort := labels["traefik.tcp.services."+tcpServiceName+".loadbalancer.server.port"]
			if tcpRule == "" || tcpPort == "" {
				continue
			}

			tcpRouter := types.TCPRouter{
				Rule:    tcpRule,
				Service: tcpServiceName,
			}
			if eps := splitEntryPoints(labels["traefik.tcp.routers."+tcpName+".entrypoints"]); len(eps) > 0 {
				tcpRouter.EntryPoints = eps
			}
			if mws := splitEntryPoints(labels["traefik.tcp.routers."+tcpName+".middlewares"]); len(mws) > 0 {
				tcpRouter.Middlewares = mws
			}
			tcpRouter.TLS = extractTCPRouterTLS(labels, tcpName)
			cfg.TCP.Routers[tcpName] = tcpRouter

			tcpServers := make([]types.TCPServer, 0, len(endpoints))
			for _, endpoint := range endpoints {
				tcpServers = append(tcpServers, types.TCPServer{
					Address: endpoint + ":" + tcpPort,
				})
			}
			tcpLB := types.TCPLoadBalancer{Servers: tcpServers}
			if td := labels["traefik.tcp.services."+tcpServiceName+".loadbalancer.terminationdelay"]; td != "" {
				if n, err := strconv.Atoi(td); err == nil {
					tcpLB.TerminationDelay = n
				}
			}
			if ppv := labels["traefik.tcp.services."+tcpServiceName+".loadbalancer.proxyprotocol.version"]; ppv != "" {
				if n, err := strconv.Atoi(ppv); err == nil {
					tcpLB.ProxyProtocol = &types.ProxyProtocol{Version: n}
				}
			}
			cfg.TCP.Services[tcpServiceName] = types.TCPService{LoadBalancer: tcpLB}
		}

		// ── UDP ────────────────────────────────────────────────────────────────

		for _, udpName := range udpRouterNames(labels) {
			udpServiceName := labels["traefik.udp.routers."+udpName+".service"]
			if udpServiceName == "" {
				udpServiceName = udpName
			}
			udpPort := labels["traefik.udp.services."+udpServiceName+".loadbalancer.server.port"]
			if udpPort == "" {
				continue
			}

			udpRouter := types.UDPRouter{Service: udpServiceName}
			if eps := splitEntryPoints(labels["traefik.udp.routers."+udpName+".entrypoints"]); len(eps) > 0 {
				udpRouter.EntryPoints = eps
			}
			cfg.UDP.Routers[udpName] = udpRouter

			udpServers := make([]types.UDPServer, 0, len(endpoints))
			for _, endpoint := range endpoints {
				udpServers = append(udpServers, types.UDPServer{
					Address: endpoint + ":" + udpPort,
				})
			}
			cfg.UDP.Services[udpServiceName] = types.UDPService{
				LoadBalancer: types.UDPLoadBalancer{Servers: udpServers},
			}
		}
	}

	if len(cfg.HTTP.Routers) == 0 && len(cfg.HTTP.Services) == 0 &&
		len(cfg.TCP.Routers) == 0 && len(cfg.TCP.Services) == 0 &&
		len(cfg.UDP.Routers) == 0 && len(cfg.UDP.Services) == 0 {
		return fmt.Errorf("generated Traefik configuration is empty")
	}
	if len(cfg.HTTP.Routers) == 0 && len(cfg.HTTP.Services) == 0 && len(cfg.HTTP.Middlewares) == 0 {
		cfg.HTTP = nil
	}
	if len(cfg.TCP.Routers) == 0 && len(cfg.TCP.Services) == 0 {
		cfg.TCP = nil
	}
	if len(cfg.UDP.Routers) == 0 && len(cfg.UDP.Services) == 0 {
		cfg.UDP = nil
	}

	data, err := configio.MarshalYAML(cfg)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o755); err != nil {
		return err
	}
	return configio.WriteAtomic(outputPath, data, 0o644)
}

func extractHealthCheck(labels map[string]string, serviceName string) *types.HealthChecks {
	// Input lookups are lowercase because label keys are normalized upstream.
	// Output field names (e.g. FollowRedirects → "followRedirects") are defined
	// by the yaml struct tags on types.HealthChecks.
	prefix := "traefik.http.services." + serviceName + ".loadbalancer.healthcheck."
	hc := &types.HealthChecks{
		Path:            labels[prefix+"path"],
		Interval:        labels[prefix+"interval"],
		Timeout:         labels[prefix+"timeout"],
		Scheme:          labels[prefix+"scheme"],
		Mode:            labels[prefix+"mode"],
		Hostname:        labels[prefix+"hostname"],
		Port:            labels[prefix+"port"],
		FollowRedirects: labels[prefix+"followredirects"],
		Method:          labels[prefix+"method"],
		Status:          labels[prefix+"status"],
	}

	headers := map[string]string{}
	for k, v := range labels {
		if strings.HasPrefix(k, prefix+"headers.") {
			headers[strings.TrimPrefix(k, prefix+"headers.")] = v
		}
	}
	if len(headers) > 0 {
		hc.Headers = headers
	}

	if hc.Path == "" && hc.Interval == "" && hc.Timeout == "" && hc.Scheme == "" && hc.Mode == "" &&
		hc.Hostname == "" && hc.Port == "" && hc.FollowRedirects == "" && hc.Method == "" && hc.Status == "" && len(hc.Headers) == 0 {
		return nil
	}
	return hc
}

func extractSticky(labels map[string]string, serviceName string) *types.Sticky {
	// Input lookups are lowercase because label keys are normalized upstream.
	prefix := "traefik.http.services." + serviceName + ".loadbalancer.sticky.cookie"
	enabled := labels[prefix]
	name := labels[prefix+".name"]
	secureStr := labels[prefix+".secure"]
	httpOnlyStr := labels[prefix+".httponly"]
	sameSite := labels[prefix+".samesite"]
	maxAgeStr := labels[prefix+".maxage"]

	if enabled == "" && name == "" && secureStr == "" && httpOnlyStr == "" && sameSite == "" && maxAgeStr == "" {
		return nil
	}

	cookie := &types.StickyCookie{}
	if name != "" {
		cookie.Name = name
	}
	if secureStr != "" {
		b := secureStr == "true"
		cookie.Secure = &b
	}
	if httpOnlyStr != "" {
		b := httpOnlyStr == "true"
		cookie.HTTPOnly = &b
	}
	if sameSite != "" {
		cookie.SameSite = sameSite
	}
	if maxAgeStr != "" {
		if n, err := strconv.Atoi(maxAgeStr); err == nil {
			cookie.MaxAge = n
		}
	}
	return &types.Sticky{Cookie: cookie}
}

// extractHTTPRouterTLS builds a RouterTLS from traefik.http.routers.<name>.tls* labels.
// Returns nil when no TLS labels are present.
func extractHTTPRouterTLS(labels map[string]string, routerName string) *types.RouterTLS {
	prefix := "traefik.http.routers." + routerName + ".tls"
	hasTLS := false
	for k := range labels {
		if strings.HasPrefix(k, prefix) {
			hasTLS = true
			break
		}
	}
	if !hasTLS {
		return nil
	}
	// bare tls=false with no sub-labels → do not enable TLS
	if v, ok := labels[prefix]; ok && v == "false" {
		hasSubLabels := false
		for k := range labels {
			if strings.HasPrefix(k, prefix+".") {
				hasSubLabels = true
				break
			}
		}
		if !hasSubLabels {
			return nil
		}
	}

	tls := &types.RouterTLS{}
	if v := labels[prefix+".certresolver"]; v != "" {
		tls.CertResolver = v
	}
	if v := labels[prefix+".options"]; v != "" {
		tls.Options = v
	}

	// tls.domains[n].main / tls.domains[n].sans
	re := regexp.MustCompile(
		`^traefik\.http\.routers\.` + regexp.QuoteMeta(routerName) + `\.tls\.domains\[(\d+)\]\.(main|sans)$`,
	)
	type domainEntry struct {
		Main string
		SANs []string
	}
	domains := map[int]*domainEntry{}
	for k, v := range labels {
		if m := re.FindStringSubmatch(k); m != nil {
			idx, _ := strconv.Atoi(m[1])
			if domains[idx] == nil {
				domains[idx] = &domainEntry{}
			}
			if m[2] == "main" {
				domains[idx].Main = v
			} else {
				domains[idx].SANs = splitEntryPoints(v)
			}
		}
	}
	if len(domains) > 0 {
		indices := make([]int, 0, len(domains))
		for i := range domains {
			indices = append(indices, i)
		}
		sort.Ints(indices)
		for _, i := range indices {
			d := domains[i]
			tls.Domains = append(tls.Domains, types.TLSDomain{Main: d.Main, SANs: d.SANs})
		}
	}

	return tls
}

// extractTCPRouterTLS builds a TCPRouterTLS from traefik.tcp.routers.<name>.tls* labels.
// Returns nil when no TLS labels are present.
func extractTCPRouterTLS(labels map[string]string, routerName string) *types.TCPRouterTLS {
	prefix := "traefik.tcp.routers." + routerName + ".tls"
	hasTLS := false
	for k := range labels {
		if strings.HasPrefix(k, prefix) {
			hasTLS = true
			break
		}
	}
	if !hasTLS {
		return nil
	}

	tls := &types.TCPRouterTLS{}
	if labels[prefix+".passthrough"] == "true" {
		tls.Passthrough = true
	}
	if v := labels[prefix+".certresolver"]; v != "" {
		tls.CertResolver = v
	}
	if v := labels[prefix+".options"]; v != "" {
		tls.Options = v
	}
	return tls
}

// extractMiddlewares parses all traefik.http.middlewares.<name>.* labels into a
// map keyed by middleware name. Dot-notation paths and [n] array indices are
// converted to nested map/slice structures. Values are coerced to bool/int/string.
func extractMiddlewares(labels map[string]string) map[string]map[string]interface{} {
	result := map[string]map[string]interface{}{}
	for _, mwName := range middlewareNames(labels) {
		prefix := "traefik.http.middlewares." + mwName + "."
		config := map[string]interface{}{}
		for k, v := range labels {
			if strings.HasPrefix(k, prefix) {
				subPath := strings.TrimPrefix(k, prefix)
				setNestedValue(config, parseLabelPath(subPath), coerceLabelValue(v))
			}
		}
		if len(config) > 0 {
			result[mwName] = config
		}
	}
	return result
}

// normalizeLabelKeys returns a new map with every key lowercased. Values are
// left untouched. Matches Traefik's docker provider, which is case-insensitive
// on label keys.
func normalizeLabelKeys(labels map[string]string) map[string]string {
	out := make(map[string]string, len(labels))
	for k, v := range labels {
		out[strings.ToLower(k)] = v
	}
	return out
}

func httpRouterNames(labels map[string]string) []string {
	set := map[string]struct{}{}
	for key := range labels {
		if strings.HasPrefix(key, "traefik.http.routers.") {
			parts := strings.Split(key, ".")
			if len(parts) >= 4 {
				set[parts[3]] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func tcpRouterNames(labels map[string]string) []string {
	set := map[string]struct{}{}
	for key := range labels {
		if strings.HasPrefix(key, "traefik.tcp.routers.") {
			parts := strings.Split(key, ".")
			if len(parts) >= 4 {
				set[parts[3]] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func middlewareNames(labels map[string]string) []string {
	set := map[string]struct{}{}
	for key := range labels {
		if strings.HasPrefix(key, "traefik.http.middlewares.") {
			parts := strings.Split(key, ".")
			if len(parts) >= 4 {
				set[parts[3]] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func udpRouterNames(labels map[string]string) []string {
	set := map[string]struct{}{}
	for key := range labels {
		if strings.HasPrefix(key, "traefik.udp.routers.") {
			parts := strings.Split(key, ".")
			if len(parts) >= 4 {
				set[parts[3]] = struct{}{}
			}
		}
	}
	names := make([]string, 0, len(set))
	for n := range set {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

func splitEntryPoints(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

// parseLabelPath converts a dot-notation path string (with optional [n] array indices)
// into a slice of path components.
// e.g. "tls.domains[0].main" → ["tls", "domains", "0", "main"]
var reBracketIndex = regexp.MustCompile(`\[(\d+)\]`)

func parseLabelPath(s string) []string {
	s = reBracketIndex.ReplaceAllString(s, ".$1")
	return strings.Split(s, ".")
}

// coerceLabelValue converts a Traefik label string to its natural Go type.
func coerceLabelValue(v string) interface{} {
	if v == "true" {
		return true
	}
	if v == "false" {
		return false
	}
	if n, err := strconv.Atoi(v); err == nil {
		return n
	}
	return v
}

// setNestedValue sets value in a nested map[string]interface{} using a path slice.
// Numeric path components create/index into []interface{} slices.
func setNestedValue(m map[string]interface{}, path []string, value interface{}) {
	if len(path) == 0 {
		return
	}
	key := path[0]
	if len(path) == 1 {
		m[key] = value
		return
	}
	// If the next component is a numeric index, the current key maps to a slice.
	if idx, err := strconv.Atoi(path[1]); err == nil {
		var sl []interface{}
		if existing, ok := m[key]; ok {
			if s, ok := existing.([]interface{}); ok {
				sl = s
			}
		}
		for len(sl) <= idx {
			sl = append(sl, nil)
		}
		if len(path) == 2 {
			sl[idx] = value
		} else {
			sub, _ := sl[idx].(map[string]interface{})
			if sub == nil {
				sub = map[string]interface{}{}
			}
			setNestedValue(sub, path[2:], value)
			sl[idx] = sub
		}
		m[key] = sl
	} else {
		sub, _ := m[key].(map[string]interface{})
		if sub == nil {
			sub = map[string]interface{}{}
		}
		setNestedValue(sub, path[1:], value)
		m[key] = sub
	}
}
