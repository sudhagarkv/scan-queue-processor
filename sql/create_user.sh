apt-get update
apt-get install gettext -y
envsubst < /sql/create_user.sql > /sql/create_user_tmp.sql
psql -U SYS -d postgres -a -f /sql/create_user_tmp.sql
