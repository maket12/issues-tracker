CREATE TABLE Users (
  `key`      BIGINT       NOT NULL AUTO_INCREMENT,
  id         VARCHAR(64)  NOT NULL,
  name       VARCHAR(100) NOT NULL,
  email      VARCHAR(255) NOT NULL,
  passwdHash VARCHAR(255) NOT NULL,
  role       BIGINT       NOT NULL,
  PRIMARY KEY (`key`),
  UNIQUE KEY uq_users_id (id),
  UNIQUE KEY uq_users_email (email),
  CONSTRAINT fk_users_role
    FOREIGN KEY (role) REFERENCES Roles (`key`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
