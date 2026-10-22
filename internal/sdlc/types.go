package sdlc

type Catalog struct {
	SchemaVersion       int                  `json:"schemaVersion"`
	Catalog             string               `json:"catalog"`
	Harnesses           []Harness            `json:"harnesses"`
	DiscoveryCandidates []DiscoveryCandidate `json:"-"`
}

type DiscoveryCatalog struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Candidates    []DiscoveryCandidate `json:"candidates"`
}

type DiscoveryCandidate struct {
	ID             string                `json:"id"`
	Executables    []CandidateExecutable `json:"executables"`
	VersionPattern string                `json:"versionPattern"`
}

type CandidateExecutable struct {
	Executable   string `json:"executable"`
	VersionProbe Probe  `json:"versionProbe"`
}

type Harness struct {
	ID               string         `json:"id"`
	Executable       string         `json:"executable"`
	VersionProbe     Probe          `json:"versionProbe"`
	VersionPattern   string         `json:"versionPattern"`
	EnumerationProbe *Probe         `json:"enumerationProbe"`
	Isolation        string         `json:"isolation"`
	Ranges           []VersionRange `json:"ranges"`
	Models           []Model        `json:"models"`
	Sources          []string       `json:"sources"`
}

type Probe struct {
	Argv           []string `json:"argv"`
	TimeoutSeconds int      `json:"timeoutSeconds"`
	Format         string   `json:"format"`
	Source         string   `json:"source"`
	IDFields       []string `json:"idFields,omitempty"`
}

type VersionRange struct {
	Minimum          string              `json:"minimum"`
	MaximumExclusive string              `json:"maximumExclusive"`
	Status           string              `json:"status"`
	Capabilities     []string            `json:"capabilities"`
	ReasoningLevels  []string            `json:"reasoningLevels"`
	Run              []string            `json:"run"`
	ModelFlag        *string             `json:"modelFlag"`
	ReasoningFlag    *string             `json:"reasoningFlag"`
	StructuredOutput *string             `json:"structuredOutput"`
	Certification    Certification       `json:"certification"`
	Invocation       *InvocationContract `json:"invocation,omitempty"`
}

// InvocationContract describes an external runner's inputs and outputs. It is
// not permission to launch a provider, publish a review, or complete a task.
type InvocationContract struct {
	SchemaVersion int               `json:"schemaVersion"`
	Actions       InvocationActions `json:"actions"`
}

type InvocationActions struct {
	Execution *InvocationAction `json:"execution,omitempty"`
	Review    *InvocationAction `json:"review,omitempty"`
}

type InvocationAction struct {
	Argv             []string                 `json:"argv"`
	WorkingDirectory string                   `json:"workingDirectory"`
	Prompt           InvocationPrompt         `json:"prompt"`
	Permissions      InvocationPermissions    `json:"permissions"`
	Output           InvocationOutputContract `json:"output"`
	Completion       InvocationCompletion     `json:"completion"`
	Publication      *InvocationPublication   `json:"publication,omitempty"`
}

type InvocationPrompt struct {
	Transport string `json:"transport"`
	MaxBytes  int    `json:"maxBytes"`
}

type InvocationPermissions struct {
	Mode             string   `json:"mode"`
	ExternalBoundary string   `json:"externalBoundary"`
	Limitations      []string `json:"limitations"`
}

type InvocationOutputContract struct {
	Normalizer string `json:"normalizer"`
	MaxBytes   int    `json:"maxBytes"`
}

type InvocationCompletion struct {
	SuccessExitCode     int    `json:"successExitCode"`
	RequiresFinalOutput bool   `json:"requiresFinalOutput"`
	TaskCompletion      string `json:"taskCompletion"`
}

type InvocationPublication struct {
	Contract         string `json:"contract"`
	Actor            string `json:"actor"`
	RequiresReadback bool   `json:"requiresReadback"`
	AuditRecord      string `json:"auditRecord"`
}

type Certification struct {
	Schema         bool     `json:"schema"`
	Argv           bool     `json:"argv"`
	FakeCLI        bool     `json:"fakeCli"`
	Resolver       bool     `json:"resolver"`
	Live           string   `json:"live"`
	Deck           string   `json:"deck"`
	TestedVersions []string `json:"testedVersions"`
	CertifiedAt    *string  `json:"certifiedAt"`
	ResultDigest   *string  `json:"resultDigest"`
}

type Model struct {
	ID              string   `json:"id"`
	ModelFamily     string   `json:"modelFamily"`
	QualityTier     string   `json:"qualityTier"`
	Entitlement     string   `json:"entitlement"`
	ReasoningLevels []string `json:"reasoningLevels"`
	Status          string   `json:"status"`
}

type Inventory struct {
	SchemaVersion int                  `json:"schemaVersion"`
	Source        string               `json:"source"`
	Catalog       string               `json:"catalog"`
	Harnesses     []HarnessObservation `json:"harnesses"`
}

type HarnessObservation struct {
	ID          string             `json:"id"`
	State       string             `json:"state"`
	Version     *string            `json:"version"`
	RangeStatus string             `json:"rangeStatus"`
	Models      []ModelObservation `json:"models"`
	Unknown     []string           `json:"unknown"`
	ProbeErrors []string           `json:"probeErrors"`
}

type ModelObservation struct {
	ID              string   `json:"id"`
	ModelFamily     *string  `json:"modelFamily"`
	QualityTier     string   `json:"qualityTier"`
	Entitlement     string   `json:"entitlement"`
	ReasoningLevels []string `json:"reasoningLevels"`
	Provenance      string   `json:"provenance"`
	Verified        bool     `json:"verified"`
}

type AnswersFile struct {
	SchemaVersion        int                  `json:"schemaVersion"`
	Seed                 string               `json:"seed"`
	ExecutionTemplate    string               `json:"executionTemplate"`
	Answers              Answers              `json:"answers"`
	OperatorCapabilities []OperatorCapability `json:"operatorCapabilities"`
	ProjectRules         []Rule               `json:"projectRules"`
}

type Answers struct {
	Authorship        string   `json:"authorship"`
	Delivery          string   `json:"delivery"`
	Risk              string   `json:"risk"`
	RoutingPreference string   `json:"routingPreference"`
	HighRiskClasses   []string `json:"highRiskClasses"`
}

type OperatorCapability struct {
	Harness         string `json:"harness"`
	Model           string `json:"model"`
	Entitlement     string `json:"entitlement"`
	AllowUnverified bool   `json:"allowUnverified"`
}

type Rule struct {
	ID            string   `json:"id"`
	Title         string   `json:"title"`
	Requirement   string   `json:"requirement"`
	Applicability string   `json:"applicability"`
	Assurance     string   `json:"assurance"`
	Owner         *string  `json:"owner"`
	Contract      *string  `json:"contract"`
	Critical      bool     `json:"critical"`
	Evidence      []string `json:"evidence,omitempty"`
}

type Seed struct {
	SchemaVersion int    `json:"schemaVersion"`
	ID            string `json:"id"`
	Title         string `json:"title"`
	Rules         []Rule `json:"rules"`
}

type ExecutionTemplate struct {
	SchemaVersion int        `json:"schemaVersion"`
	ID            string     `json:"id"`
	Roles         []Role     `json:"roles"`
	Escalation    Escalation `json:"escalation"`
}

type Role struct {
	ID                string      `json:"id"`
	MinimumQuality    string      `json:"minimumQuality"`
	PreferEntitlement string      `json:"preferEntitlement"`
	Reasoning         string      `json:"reasoning"`
	Candidates        []Candidate `json:"candidates"`
	Fallback          *string     `json:"fallback"`
	Constraints       []string    `json:"constraints"`
}

type Candidate struct {
	Harness string `json:"harness"`
	Model   string `json:"model"`
}

type Escalation struct {
	AfterReviewRounds int    `json:"afterReviewRounds"`
	NextRole          string `json:"nextRole"`
}

type Binding struct {
	Role        string   `json:"role"`
	Status      string   `json:"status"`
	Harness     *string  `json:"harness"`
	Model       *string  `json:"model"`
	ModelFamily *string  `json:"modelFamily"`
	Reasoning   *string  `json:"reasoning"`
	Provenance  string   `json:"provenance"`
	Fallback    *string  `json:"fallback"`
	Constraints []string `json:"constraints"`
	Reasons     []string `json:"reasons"`
}
