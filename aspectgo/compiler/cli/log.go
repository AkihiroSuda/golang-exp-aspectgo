package cli

import (
	"bytes"
	"text/template"

	log "github.com/cihub/seelog"
)

// Initialize cihub/seelog.
func initLog(debug bool) {
	configTmpl := `
<seelog type="sync" minlevel="{{.minlevel}}">
    <outputs formatid="main">
        <console/>
    </outputs>
    <formats>
        <format id="main" format="[AG-%LEV] %Date(15:04:05.00): %Msg (at %File:%Line) %n"/>
    </formats>
</seelog>`
	minlevel := "info"
	if debug {
		minlevel = "debug"
	}
	t := template.New("t")
	m := map[string]string{"minlevel": minlevel}
	template.Must(t.Parse(configTmpl))
	var b bytes.Buffer
	if err := t.Execute(&b, m); err != nil {
		panic(err)
	}
	logger, err := log.LoggerFromConfigAsBytes(b.Bytes())
	if err != nil {
		panic(err)
	}
	log.ReplaceLogger(logger)
}
