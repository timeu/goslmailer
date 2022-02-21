package msteams

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net/http"
	"os"
	"strconv"
	"text/template"
	"time"

	"github.com/pja237/goslmailer/internal/lookup"
	"github.com/pja237/goslmailer/internal/message"
	"github.com/pja237/goslmailer/internal/slurmjob"
	"github.com/pja237/goslmailer/internal/spool"
)

func NewConnector(conf map[string]string) (*Connector, error) {
	// here we need some test if the connectors "minimal" configuration is satisfied, e.g. must have url at minimum
	c := Connector{
		name:                 conf["name"],
		url:                  conf["url"],
		renderToFile:         conf["renderToFile"],
		spoolDir:             conf["spoolDir"],
		adaptiveCardTemplate: conf["adaptiveCardTemplate"],
		useLookup:            conf["useLookup"],
	}
	// if renderToFile=="no" or "spool" then spoolDir must not be empty
	switch c.renderToFile {
	case "no", "spool":
		if c.spoolDir == "" {
			return nil, errors.New("spoolDir must be defined, aborting")
		}
	}
	return &c, nil
}

func (c *Connector) msteamsRenderCardTemplate(j *slurmjob.JobContext, userid string, buf *bytes.Buffer) error {

	var x = struct {
		Job    slurmjob.JobContext
		UserID string
	}{
		*j,
		userid,
	}

	f, err := os.ReadFile(c.adaptiveCardTemplate)
	if err != nil {
		return err
	}
	t := template.Must(template.New("AdaptiveCard").Parse(string(f)))
	err = t.Execute(buf, x)
	if err != nil {
		return err
	}
	return nil
}

func (c *Connector) dumpConnector(l *log.Logger) {
	l.Printf("msteams.dumpConnector: name: %q\n", c.name)
	l.Printf("msteams.dumpConnector: url: %q\n", c.url)
	l.Printf("msteams.dumpConnector: renderToFile: %q\n", c.renderToFile)
	l.Printf("msteams.dumpConnector: spoolDir: %q\n", c.spoolDir)
	l.Printf("msteams.dumpConnector: adaptiveCardTemplate: %q\n", c.adaptiveCardTemplate)
	l.Printf("msteams.dumpConnector: useLookup: %q\n", c.useLookup)
	l.Println("................................................................................")

}

func (c *Connector) SendMessage(mp *message.MessagePack, useSpool bool, l *log.Logger) error {

	var (
		e       error = nil
		outFile string
		dts     bool = false // DumpToSpool
	)

	l.Println("................... sendToMSTeams START ........................................")

	// lookup the end-system userid from the one sent by slurm (if lookup is set in "useLookup" config param)
	enduser := lookup.ExtLookupUser(mp.TargetUser, c.useLookup)
	l.Printf("Looked up with %q %s -> %s\n", c.useLookup, mp.TargetUser, enduser)

	// prepare outfile name
	t := strconv.FormatInt(time.Now().UnixNano(), 10)
	l.Printf("MsTeams time: %s\n", t)
	outFile = "rendered-" + mp.JobContext.SLURM_JOB_ID + "-" + enduser + "-" + t + ".json"

	l.Printf("MsTeams sending to targetUserID: %s\n", enduser)

	// debug purposes
	c.dumpConnector(l)

	// buffer to place rendered json in
	buffer := bytes.Buffer{}
	err := c.msteamsRenderCardTemplate(mp.JobContext, enduser, &buffer)
	if err != nil {
		return err
	}

	// this can be: "yes", "spool", anythingelse
	switch c.renderToFile {
	case "yes":
		// render json template to working directory - debug purposes
		res, err := io.ReadAll(&buffer)
		// test err and do something!
		e = err
		os.WriteFile(outFile, res, 0644)
		l.Printf("MsTeams send to file: %s\n", outFile)
	case "spool":
		// deposit GOB to spoolDir if allowed
		if useSpool {
			err = spool.DepositToSpool(c.spoolDir, mp)
			if err != nil {
				l.Printf("DepositToSpool Failed!\n")
				return err
			}
		}
	default:
		// handle here "too many requests" 4xx and place the rendered message to spool dir to be picked up later by the "throttler"
		resp, err := http.Post(c.url, "application/json", &buffer)
		if err != nil {
			l.Printf("http.Post Failed!\n")
			dts = true
			//return err
			e = err
		} else {
			l.Printf("MsTeams RESPONSE Status: %s\n", resp.Status)
			switch resp.StatusCode {
			case 429:
				l.Printf("429 received.\n")
				dts = true
			default:
				l.Printf("Send OK!\n")
			}
		}
	}

	// either http.Post failed, or it got 429, backing off to spool
	if dts && useSpool {
		l.Printf("Backing off to spool.\n")
		err = spool.DepositToSpool(c.spoolDir, mp)
		if err != nil {
			l.Printf("DepositToSpool Failed!\n")
			return err
		}
	}

	l.Println("................... sendToMSTeams END ..........................................")

	return e
}
