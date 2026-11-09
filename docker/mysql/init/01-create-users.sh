#!/bin/bash
# Runs once, when the MySQL data volume is first initialised.
# Creates two non-super accounts, both restricted to the application database.
set -eu

: "${MYSQL_DATABASE:?}" "${MIGRATOR_USER:?}" "${MIGRATOR_PASSWORD:?}" "${APP_USER:?}" "${APP_PASSWORD:?}"

mysql -uroot -p"${MYSQL_ROOT_PASSWORD}" <<SQL
CREATE USER '${MIGRATOR_USER}'@'%' IDENTIFIED BY '${MIGRATOR_PASSWORD}';
CREATE USER '${APP_USER}'@'%'      IDENTIFIED BY '${APP_PASSWORD}';

-- Schema owner for migrations: full control of ONE database, no GRANT OPTION, no global privileges.
GRANT ALL PRIVILEGES ON \`${MYSQL_DATABASE}\`.* TO '${MIGRATOR_USER}'@'%';

-- Backend: read and write data only. It cannot create, alter or drop tables.
GRANT SELECT, INSERT, UPDATE, DELETE ON \`${MYSQL_DATABASE}\`.* TO '${APP_USER}'@'%';

FLUSH PRIVILEGES;
SQL
