package sdlc

import (
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"strconv"
)

const webHTML = `<!doctype html>
<html lang="en"><head><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1">
<title>noise sdlc</title><style>
:root{font:16px/1.45 system-ui,sans-serif;color:#e8edf2;background:#111820}body{max-width:940px;margin:0 auto;padding:32px 20px}h1{margin-bottom:4px}p{color:#aebbc8}form,pre{background:#19232e;border:1px solid #304151;border-radius:12px;padding:20px}label{display:block;margin:14px 0 5px}select,input{width:100%;box-sizing:border-box;padding:10px;border-radius:7px;border:1px solid #526779;background:#101820;color:#eef}button{margin:20px 8px 0 0;padding:10px 16px;border:0;border-radius:7px;background:#63d6a3;color:#092116;font-weight:700;cursor:pointer}button.secondary{background:#34495c;color:#eef}pre{overflow:auto;white-space:pre-wrap}.status{font-family:ui-monospace,monospace;color:#63d6a3}small{color:#91a2b3}
</style></head><body><h1>noise sdlc</h1><p>Choose policy intent once; review exact harness/model routes before adoption.</p><div id="sync" class="status">Loading Deck sync state…</div><form id="questions"></form><pre id="result">No plan yet.</pre>
<script>
let boot,latest;const q=document.querySelector('#questions'),out=document.querySelector('#result');
async function start(){boot=await(await fetch('/api/bootstrap')).json();document.querySelector('#sync').textContent=boot.deckSync.state==='disabled'?'Standalone mode · catalog '+boot.catalog:'Deck: '+boot.deckSync.state+' · catalog '+boot.catalog;for(const item of boot.interaction.questions){const l=document.createElement('label');l.textContent=item.label;l.htmlFor=item.id;q.appendChild(l);let el;if(item.type==='choice'){el=document.createElement('select');for(const c of item.choices){const o=document.createElement('option');o.value=c;o.textContent=c;el.appendChild(o)}}else{el=document.createElement('input');el.placeholder='comma-separated'}el.id=item.id;el.value=Array.isArray(item.default)?item.default.join(', '):item.default;q.appendChild(el)}const plan=document.createElement('button');plan.type='button';plan.textContent='Build plan';plan.onclick=build;q.appendChild(plan);const adopt=document.createElement('button');adopt.type='button';adopt.className='secondary';adopt.textContent='Adopt reviewed plan';adopt.onclick=install;q.appendChild(adopt)}
function values(){const a={};for(const item of boot.interaction.questions){let value=document.querySelector('#'+item.id).value;if(item.type==='string-list')value=value.split(',').map(x=>x.trim()).filter(Boolean);a[item.id]=value}return a}
async function build(){const r=await fetch('/api/plan',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({answers:values()})});latest=await r.json();out.textContent=JSON.stringify(latest,null,2)}
async function install(){if(!latest||!latest.planDigest){out.textContent='Build and review a plan first.';return}if(!confirm('Install these exact managed artifacts?'))return;const r=await fetch('/api/adopt',{method:'POST',headers:{'content-type':'application/json'},body:JSON.stringify({answers:values(),planDigest:latest.planDigest,actionToken:boot.actionToken})});out.textContent=JSON.stringify(await r.json(),null,2)}
start().catch(e=>out.textContent=String(e));
</script></body></html>`

type webServer struct {
	target      string
	root        string
	baseline    AnswersFile
	inventory   Inventory
	actionToken string
	catalog     Catalog
	template    ExecutionTemplate
}

func ServeWeb(host string, port int, target, root string, baseline AnswersFile) error {
	if !oneOf(host, "127.0.0.1", "::1", "localhost") {
		return fmt.Errorf("the incubation web UI binds to loopback only")
	}
	catalog, err := LoadCatalog(root)
	if err != nil {
		return err
	}
	template, err := LoadTemplate(root)
	if err != nil {
		return err
	}
	token, err := randomToken()
	if err != nil {
		return err
	}
	server := &webServer{
		target: target, root: root, baseline: baseline,
		inventory:   DiscoverInventory(catalog, baseline.OperatorCapabilities, target),
		actionToken: token, catalog: catalog, template: template,
	}
	listener, err := net.Listen("tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	fmt.Printf("noise sdlc web UI: http://%s\n", listener.Addr())
	return http.Serve(listener, server)
}

func (server *webServer) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method == http.MethodGet && request.URL.Path == "/" {
		writer.Header().Set("content-type", "text/html; charset=utf-8")
		writer.Header().Set("cache-control", "no-store")
		writer.Header().Set("content-security-policy", "default-src 'self'; script-src 'unsafe-inline'; style-src 'unsafe-inline'; connect-src 'self'; frame-ancestors 'self'")
		_, _ = io.WriteString(writer, webHTML)
		return
	}
	var body map[string]any
	if request.Method == http.MethodPost {
		mediaType, _, err := mime.ParseMediaType(request.Header.Get("content-type"))
		if err != nil || mediaType != "application/json" {
			writeJSON(writer, http.StatusUnsupportedMediaType, map[string]string{"error": "content-type must be application/json"})
			return
		}
		request.Body = http.MaxBytesReader(writer, request.Body, 1_048_576)
		decoder := json.NewDecoder(request.Body)
		if err := decoder.Decode(&body); err != nil {
			writeJSON(writer, http.StatusBadRequest, map[string]string{"error": "invalid JSON"})
			return
		}
	}
	status, response, err := server.dispatch(request.Method, request.URL.Path, body)
	if err != nil {
		writeJSON(writer, http.StatusBadRequest, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(writer, status, response)
}

func (server *webServer) dispatch(method, path string, body map[string]any) (int, any, error) {
	switch {
	case method == http.MethodGet && path == "/api/bootstrap":
		digest, _ := Digest(server.catalog)
		return http.StatusOK, map[string]any{
			"schemaVersion": 1, "catalog": server.catalog.Catalog, "catalogDigest": digest,
			"interaction": InteractionContract(), "deckSync": DeckSyncStatus(server.target, server.catalog),
			"actionToken": server.actionToken,
		}, nil
	case method == http.MethodGet && path == "/api/deck-sync":
		return http.StatusOK, DeckSyncBundle(server.catalog, server.template), nil
	case method == http.MethodGet && path == "/api/onboarding":
		return http.StatusOK, OnboardingStatus(server.target), nil
	case method == http.MethodPost && path == "/api/deck-receipt":
		if stringValue(body["actionToken"]) != server.actionToken {
			return 0, nil, fmt.Errorf("Deck receipt requires the loopback action token")
		}
		receipt, ok := body["receipt"].(map[string]any)
		if !ok {
			return 0, nil, fmt.Errorf("Deck receipt requires the loopback action token")
		}
		accepted, err := AcceptDeckReceipt(receipt, server.target, server.catalog)
		return http.StatusOK, accepted, err
	case method == http.MethodPost && (path == "/api/plan" || path == "/api/adopt"):
		answers, err := server.mergedAnswers(body["answers"])
		if err != nil {
			return 0, nil, err
		}
		built, err := BuildPlan(server.target, server.root, answers, &server.inventory)
		if err != nil {
			return 0, nil, err
		}
		encoded, _ := CanonicalBytes(built.Plan)
		digest := DigestBytes(encoded)
		if path == "/api/plan" {
			return http.StatusOK, map[string]any{"planDigest": digest, "plan": built.Plan}, nil
		}
		if stringValue(body["actionToken"]) != server.actionToken || stringValue(body["planDigest"]) != digest {
			return 0, nil, fmt.Errorf("adoption confirmation does not match the current plan")
		}
		if err := Adopt(server.target, built.Outputs); err != nil {
			return 0, nil, err
		}
		verification, err := Verify(server.target)
		return http.StatusOK, map[string]any{"adopted": true, "planDigest": digest, "verification": verification}, err
	default:
		return 0, nil, fmt.Errorf("unknown web API route")
	}
}

func (server *webServer) mergedAnswers(raw any) (AnswersFile, error) {
	values, ok := raw.(map[string]any)
	if !ok {
		return AnswersFile{}, fmt.Errorf("request requires an answers object")
	}
	expected := map[string]bool{
		"authorship": true, "delivery": true, "risk": true,
		"routingPreference": true, "highRiskClasses": true,
	}
	if len(values) != len(expected) {
		return AnswersFile{}, fmt.Errorf("web answers contain unknown or missing fields")
	}
	for key := range values {
		if !expected[key] {
			return AnswersFile{}, fmt.Errorf("web answers contain unknown or missing fields")
		}
	}
	encoded, _ := json.Marshal(values)
	answers := server.baseline
	if err := json.Unmarshal(encoded, &answers.Answers); err != nil {
		return AnswersFile{}, fmt.Errorf("web answers contain unknown or missing fields")
	}
	if err := ValidateAnswers(answers); err != nil {
		return AnswersFile{}, err
	}
	return answers, nil
}

func randomToken() (string, error) {
	value := make([]byte, 24)
	if _, err := rand.Read(value); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value), nil
}

func writeJSON(writer http.ResponseWriter, status int, value any) {
	payload, err := CanonicalBytes(value)
	if err != nil {
		http.Error(writer, err.Error(), http.StatusInternalServerError)
		return
	}
	writer.Header().Set("content-type", "application/json; charset=utf-8")
	writer.Header().Set("cache-control", "no-store")
	writer.Header().Set("content-security-policy", "default-src 'none'; frame-ancestors 'self'")
	writer.WriteHeader(status)
	_, _ = writer.Write(payload)
}
