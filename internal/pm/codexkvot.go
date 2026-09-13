package pm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os/exec"
	"strings"
	"time"

	"github.com/mazen160/backlog/internal/timeutil"
)

// codexKvotTimeout håller avläsningen kort. Kvoten är en trevlighet i vyn, och
// får aldrig hålla kvar en körning som i övrigt är klar.
const codexKvotTimeout = 15 * time.Second

// Fönstren kommer som varaktighet i minuter, inte som namn. Vilket fönster som
// är primary skiljer mellan konton, så PM mappar på minuterna.
const (
	codexFemTimmarMin = 300
	codexSjuDagarMin  = 10080
)

type codexKvotfonster struct {
	UsedPercent        float64 `json:"usedPercent"`
	WindowDurationMins int64   `json:"windowDurationMins"`
	ResetsAt           int64   `json:"resetsAt"`
}

type codexKvotsnapshot struct {
	LimitID   string            `json:"limitId"`
	Primary   *codexKvotfonster `json:"primary"`
	Secondary *codexKvotfonster `json:"secondary"`
	PlanType  string            `json:"planType"`
}

type codexKvotsvar struct {
	ID     int `json:"id"`
	Result struct {
		RateLimits codexKvotsnapshot `json:"rateLimits"`
	} `json:"result"`
}

// codexKorare kör app-servern. Provet byter ut den, så inget prov startar
// riktiga codex.
type codexKorare func(ctx context.Context, kommando string, indata string) ([]byte, error)

var korCodexAppServer codexKorare = func(ctx context.Context, kommando string, indata string) ([]byte, error) {
	cmd := exec.CommandContext(ctx, kommando, "app-server")
	// Stderr kastas: app-servern klagar på sandlådan även när svaret är rätt.
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	ut, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	// Stdin hålls öppen tills svaret kommit. Stängs den direkt hinner
	// app-servern avsluta innan den svarat, och då kommer ingen kvot alls.
	defer func() {
		in.Close()
		_ = cmd.Wait()
	}()
	if _, err := io.WriteString(in, indata); err != nil {
		return nil, err
	}

	var svar bytes.Buffer
	lasare := bufio.NewScanner(ut)
	lasare.Buffer(make([]byte, 0, 64*1024), 1<<20)
	for lasare.Scan() {
		rad := lasare.Bytes()
		svar.Write(rad)
		svar.WriteByte('\n')
		if bytes.Contains(rad, []byte(`"rateLimits"`)) {
			break
		}
	}
	return svar.Bytes(), lasare.Err()
}

// CodexKvot frågar codex app-server om kontots kvotläge. Strömmen från
// codex exec bär ingen kvot, se docen "Codex kvotläge: tre vägar och en
// rekommendation". Ett fel ger nil, för vyn klarar sig utan siffran.
func CodexKvot(ctx context.Context, kommando string) *Anvandning {
	if strings.TrimSpace(kommando) == "" {
		return nil
	}
	ctx, avbryt := context.WithTimeout(ctx, codexKvotTimeout)
	defer avbryt()

	indata := strings.Join([]string{
		`{"id":1,"method":"initialize","params":{"clientInfo":{"name":"backlog-pm","title":"backlog-pm","version":"1"}}}`,
		`{"method":"initialized","params":null}`,
		`{"id":2,"method":"account/rateLimits/read","params":null}`,
		"",
	}, "\n")
	ut, err := korCodexAppServer(ctx, kommando, indata)
	if err != nil {
		return nil
	}
	return tolkaCodexKvot(ut)
}

func tolkaCodexKvot(ut []byte) *Anvandning {
	lasare := bufio.NewReader(bytes.NewReader(ut))
	for {
		rad, err := lasare.ReadBytes('\n')
		if len(rad) > 0 {
			if lage := kvotUrCodexRad(rad); lage != nil {
				return lage
			}
		}
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return nil
		}
	}
}

func kvotUrCodexRad(rad []byte) *Anvandning {
	if !bytes.Contains(rad, []byte("rateLimits")) {
		return nil
	}
	var svar codexKvotsvar
	if err := json.Unmarshal(bytes.TrimSpace(rad), &svar); err != nil {
		return nil
	}
	snapshot := svar.Result.RateLimits
	lage := Anvandning{Kvottyp: snapshot.LimitID, AvlastAt: timeutil.Now()}
	for _, fonster := range []*codexKvotfonster{snapshot.Primary, snapshot.Secondary} {
		if fonster == nil {
			continue
		}
		kvot := &Kvotfonster{
			Andel: fonster.UsedPercent / 100,
			// Codex räknar i sekunder, PM i nanosekunder.
			NollstallsAt: fonster.ResetsAt * int64(time.Second),
		}
		if kvot.Andel < 0 || kvot.Andel > 1 {
			continue
		}
		switch fonster.WindowDurationMins {
		case codexFemTimmarMin:
			lage.FemTimmar = kvot
		case codexSjuDagarMin:
			lage.SjuDagar = kvot
		}
	}
	if lage.FemTimmar == nil && lage.SjuDagar == nil {
		return nil
	}
	if lage.Kvottyp == "" {
		lage.Kvottyp = "codex"
	}
	return &lage
}
