package types

type DynamicConfig struct {
	HTTP *HTTPConfig `yaml:"http,omitempty"`
	TCP  *TCPConfig  `yaml:"tcp,omitempty"`
	UDP  *UDPConfig  `yaml:"udp,omitempty"`
}

type HTTPConfig struct {
	Routers     map[string]HTTPRouter             `yaml:"routers,omitempty"`
	Middlewares map[string]map[string]interface{} `yaml:"middlewares,omitempty"`
	Services    map[string]HTTPService            `yaml:"services,omitempty"`
}

type HTTPRouter struct {
	Rule        string     `yaml:"rule,omitempty"`
	EntryPoints []string   `yaml:"entryPoints,omitempty"`
	Middlewares []string   `yaml:"middlewares,omitempty"`
	Service     string     `yaml:"service,omitempty"`
	Priority    int        `yaml:"priority,omitempty"`
	TLS         *RouterTLS `yaml:"tls,omitempty"`
}

type RouterTLS struct {
	CertResolver string      `yaml:"certResolver,omitempty"`
	Options      string      `yaml:"options,omitempty"`
	Domains      []TLSDomain `yaml:"domains,omitempty"`
}

type TLSDomain struct {
	Main string   `yaml:"main,omitempty"`
	SANs []string `yaml:"sans,omitempty"`
}

type HTTPService struct {
	LoadBalancer HTTPLoadBalancer `yaml:"loadBalancer,omitempty"`
}

type HTTPLoadBalancer struct {
	Servers            []HTTPServer        `yaml:"servers,omitempty"`
	PassHostHeader     *bool               `yaml:"passHostHeader,omitempty"`
	ResponseForwarding *ResponseForwarding `yaml:"responseForwarding,omitempty"`
	HealthCheck        *HealthChecks       `yaml:"healthCheck,omitempty"`
	Sticky             *Sticky             `yaml:"sticky,omitempty"`
}

type ResponseForwarding struct {
	FlushInterval string `yaml:"flushInterval,omitempty"`
}

type Sticky struct {
	Cookie *StickyCookie `yaml:"cookie,omitempty"`
}

type StickyCookie struct {
	Name     string `yaml:"name,omitempty"`
	Secure   *bool  `yaml:"secure,omitempty"`
	HTTPOnly *bool  `yaml:"httpOnly,omitempty"`
	SameSite string `yaml:"sameSite,omitempty"`
	MaxAge   int    `yaml:"maxAge,omitempty"`
}

type HTTPServer struct {
	URL string `yaml:"url,omitempty"`
}

type HealthChecks struct {
	Path            string            `yaml:"path,omitempty"`
	Interval        string            `yaml:"interval,omitempty"`
	Timeout         string            `yaml:"timeout,omitempty"`
	Scheme          string            `yaml:"scheme,omitempty"`
	Mode            string            `yaml:"mode,omitempty"`
	Hostname        string            `yaml:"hostname,omitempty"`
	Port            string            `yaml:"port,omitempty"`
	FollowRedirects string            `yaml:"followRedirects,omitempty"`
	Method          string            `yaml:"method,omitempty"`
	Status          string            `yaml:"status,omitempty"`
	Headers         map[string]string `yaml:"headers,omitempty"`
}

type TCPConfig struct {
	Routers  map[string]TCPRouter  `yaml:"routers,omitempty"`
	Services map[string]TCPService `yaml:"services,omitempty"`
}

type TCPRouter struct {
	Rule        string        `yaml:"rule,omitempty"`
	Service     string        `yaml:"service,omitempty"`
	EntryPoints []string      `yaml:"entryPoints,omitempty"`
	Middlewares []string      `yaml:"middlewares,omitempty"`
	TLS         *TCPRouterTLS `yaml:"tls,omitempty"`
}

type TCPRouterTLS struct {
	Passthrough  bool   `yaml:"passthrough,omitempty"`
	CertResolver string `yaml:"certResolver,omitempty"`
	Options      string `yaml:"options,omitempty"`
}

type TCPService struct {
	LoadBalancer TCPLoadBalancer `yaml:"loadBalancer,omitempty"`
}

type TCPLoadBalancer struct {
	Servers          []TCPServer    `yaml:"servers,omitempty"`
	TerminationDelay int            `yaml:"terminationDelay,omitempty"`
	ProxyProtocol    *ProxyProtocol `yaml:"proxyProtocol,omitempty"`
}

type ProxyProtocol struct {
	Version int `yaml:"version,omitempty"`
}

type TCPServer struct {
	Address string `yaml:"address,omitempty"`
}

type UDPConfig struct {
	Routers  map[string]UDPRouter  `yaml:"routers,omitempty"`
	Services map[string]UDPService `yaml:"services,omitempty"`
}

type UDPRouter struct {
	EntryPoints []string `yaml:"entryPoints,omitempty"`
	Service     string   `yaml:"service,omitempty"`
}

type UDPService struct {
	LoadBalancer UDPLoadBalancer `yaml:"loadBalancer,omitempty"`
}

type UDPLoadBalancer struct {
	Servers []UDPServer `yaml:"servers,omitempty"`
}

type UDPServer struct {
	Address string `yaml:"address,omitempty"`
}
