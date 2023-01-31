ROOT="$(git rev-parse --show-toplevel)"
DB=$(python3 -c "import json; print(json.loads(open('$ROOT/config.json').read().encode().decode('utf-8-sig'))['db']['name'])")
UNAME="$(uname)"
COMMAND1="\copy (select * from developers inner join developer_snapshots on developer_snapshots.developer_id = developers.id left join games on developers.username = ANY(games.developers) where not developers.disabled) to '$(pwd)/$(date +%F)_sample.csv' (FORMAT CSV, HEADER TRUE, DELIMITER ',', ENCODING 'UTF8');"
COMMAND2="\copy (select * from steam_apps where not bare and weighted_score is not null) to '$(pwd)/$(date +%F)_steamapps.csv' (FORMAT CSV, HEADER TRUE, DELIMITER ',', ENCODING 'UTF8');"
if [ "$UNAME" == "Linux" ]; then
  sudo -u postgres psql $DB -c "$COMMAND1"
  sudo -u postgres psql $DB -c "$COMMAND2"
elif [ "$UNAME" == "Darwin" ]; then
  psql $DB -c "$COMMAND1"
  psql $DB -c "$COMMAND2"
fi
