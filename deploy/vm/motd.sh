#!/bin/sh
# Login banner: ari says it is here, what it may do, and how to stop it.
cat <<'EOF'

  ari looks after toyd on this machine.
  It only runs repairs from /opt/ari/catalogue/toyd.json, each with an undo,
  and escalates anything else to you. It cannot reach the internet.

    watch it:  ari watch -data /var/lib/ari/data
    report:    ari report -data /var/lib/ari/data
    stop it:   ari stop -data /var/lib/ari/data   (stays stopped until you remove the STOP file)

EOF
/opt/ari/bin/ari status -data /var/lib/ari/data 2>/dev/null | sed 's/^/  /'
echo
