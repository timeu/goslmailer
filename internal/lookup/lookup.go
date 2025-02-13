package lookup

import (
	"log"
	"os/exec"
	"strings"
)

func ExtLookupUser(user string, lookup string) (u string) {

	// todo: this whole package needs some love, quite some love
	switch lookup {
	case "GECOS":
		u = lookupGECOS(user)
	default:
		u = user
	}

	return u
}

func lookupGECOS(u string) string {

	out, err := exec.Command("/usr/bin/getent", "passwd", u).Output()
	if err != nil {
		log.Fatalln("Getent lookup FAILED. Aborting!")
	}
	fields := strings.Split(string(out), ":")

	return fields[4]
}
