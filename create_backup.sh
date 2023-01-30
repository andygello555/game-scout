DB=$(python3 -c "import json; print(json.loads(open('config.json').read().encode().decode('utf-8-sig'))['db']['name'])")
pg_dump $DB > "game_scout_db-$(date +%F).sql"
