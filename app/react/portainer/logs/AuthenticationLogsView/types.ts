export enum AuthMethodType {
  Unknown = 0,
  Internal = 1,
  LDAP = 2,
  OAuth = 3,
}

export enum ActivityType {
  AuthSuccess = 1,
  AuthFailure = 2,
  Logout = 3,
}

export interface AuthLog {
  id: string;
  timestamp: number;
  username: string;
  context: AuthMethodType;
  type: ActivityType;
  origin: string;
  statusCode: number;
  userId?: number;
  rateLimited?: boolean;
  suppressedRateLimited?: number;
}
