DROP USER IF EXISTS scanner;
CREATE USER scanner WITH PASSWORD '$SCANNER_PASSWORD';
DROP DATABASE IF EXISTS scanner;
CREATE DATABASE scanner WITH OWNER scanner;
GRANT ALL PRIVILEGES ON DATABASE scanner TO scanner;
