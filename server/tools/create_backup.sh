ROOT="$(git rev-parse --show-toplevel)"
DB=$(python3 -c "import json; print(json.loads(open('$ROOT/config.json').read().encode().decode('utf-8-sig'))['db']['name'])")
FILENAME="game_scout_db-$(date +%F).sql"
UNAME="$(uname)"
if [ "$UNAME" == "Linux" ]; then
  sudo -u postgres pg_dump $DB > "$FILENAME"
elif [ "$UNAME" == "Darwin" ]; then
  pg_dump $DB > "$FILENAME"
fi
