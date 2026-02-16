# API / CLI 最小契约

## API
- /version, /healthz
- /chats, /chats/{chat_id}, /chats/batch-delete
- /agent/process
- /cron/jobs 系列
- /models 系列
- /envs 系列
- /skills 系列
- /workspace/files, /workspace/files/{file_path}
- /workspace/export, /workspace/import
- /config/channels 系列

## CLI
- copaw app start
- copaw chats list/create/get/delete/send
- copaw cron list/create/update/delete/pause/resume/run/state
- copaw models list/config/active-get/active-set
- copaw env list/set/delete
- copaw skills list/create/enable/disable/delete
- copaw workspace ls/cat/put/rm/export/import
- copaw channels list/types/get/set
