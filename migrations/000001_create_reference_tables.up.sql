CREATE TABLE Roles (
  `key` BIGINT       NOT NULL AUTO_INCREMENT,
  id    VARCHAR(64)  NOT NULL,
  name  VARCHAR(100) NOT NULL,
  PRIMARY KEY (`key`),
  UNIQUE KEY uq_roles_id (id),
  UNIQUE KEY uq_roles_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE RolesPermissions (
  `key`      BIGINT       NOT NULL AUTO_INCREMENT,
  role       BIGINT       NOT NULL,
  permission VARCHAR(100) NOT NULL,
  PRIMARY KEY (`key`),
  UNIQUE KEY uq_rolespermissions_role_permission (role, permission),
  CONSTRAINT fk_rolespermissions_role
    FOREIGN KEY (role) REFERENCES Roles (`key`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE Statuses (
  `key` BIGINT       NOT NULL AUTO_INCREMENT,
  id    VARCHAR(64)  NOT NULL,
  name  VARCHAR(100) NOT NULL,
  PRIMARY KEY (`key`),
  UNIQUE KEY uq_statuses_id (id),
  UNIQUE KEY uq_statuses_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE Priorities (
  `key`       BIGINT       NOT NULL AUTO_INCREMENT,
  id          VARCHAR(64)  NOT NULL,
  name        VARCHAR(100) NOT NULL,
  description VARCHAR(500) NULL,
  PRIMARY KEY (`key`),
  UNIQUE KEY uq_priorities_id (id),
  UNIQUE KEY uq_priorities_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE IssueTypes (
  `key`       BIGINT       NOT NULL AUTO_INCREMENT,
  id          VARCHAR(64)  NOT NULL,
  name        VARCHAR(100) NOT NULL,
  description VARCHAR(500) NULL,
  PRIMARY KEY (`key`),
  UNIQUE KEY uq_issuetypes_id (id),
  UNIQUE KEY uq_issuetypes_name (name)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
