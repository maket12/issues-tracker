CREATE TABLE TransitionRules (
  `key`       BIGINT  NOT NULL AUTO_INCREMENT,
  `from`      BIGINT  NOT NULL,
  `to`        BIGINT  NOT NULL,
  allowAuthor BOOLEAN NOT NULL DEFAULT FALSE,
  PRIMARY KEY (`key`),
  UNIQUE KEY uq_transitionrules_from_to (`from`, `to`),
  KEY idx_transitionrules_to (`to`),
  CONSTRAINT fk_transitionrules_from FOREIGN KEY (`from`) REFERENCES Statuses (`key`) ON DELETE CASCADE,
  CONSTRAINT fk_transitionrules_to   FOREIGN KEY (`to`)   REFERENCES Statuses (`key`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;

CREATE TABLE TransitionRules_Roles (
  TransitionRulekey BIGINT NOT NULL,
  Rolekey           BIGINT NOT NULL,
  PRIMARY KEY (TransitionRulekey, Rolekey),
  KEY idx_transitionrules_roles_role (Rolekey),
  CONSTRAINT fk_trr_rule FOREIGN KEY (TransitionRulekey) REFERENCES TransitionRules (`key`) ON DELETE CASCADE,
  CONSTRAINT fk_trr_role FOREIGN KEY (Rolekey)           REFERENCES Roles (`key`) ON DELETE CASCADE
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4;
