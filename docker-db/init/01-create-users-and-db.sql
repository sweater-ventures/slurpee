-- Create application roles
CREATE ROLE slurpee_admin LOGIN PASSWORD 'securepassword123';
CREATE ROLE slurpee LOGIN PASSWORD 'userpassword456';

-- Create the application database owned by the admin role
CREATE DATABASE slurpee OWNER slurpee_admin;

-- Connect to the new database and configure permissions
\connect slurpee

-- Grant schema privileges
GRANT ALL ON SCHEMA public TO slurpee_admin;
GRANT USAGE ON SCHEMA public TO slurpee;

-- Grant table and sequence privileges for existing objects
GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO slurpee;
GRANT USAGE, SELECT ON ALL SEQUENCES IN SCHEMA public TO slurpee;

-- Set default privileges so future objects created by slurpee_admin
-- are automatically accessible to the slurpee application role
ALTER DEFAULT PRIVILEGES FOR ROLE slurpee_admin IN SCHEMA public
    GRANT SELECT, INSERT, UPDATE, DELETE ON TABLES TO slurpee;

ALTER DEFAULT PRIVILEGES FOR ROLE slurpee_admin IN SCHEMA public
    GRANT USAGE, SELECT ON SEQUENCES TO slurpee;
