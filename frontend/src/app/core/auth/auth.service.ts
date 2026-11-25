import { HttpClient } from '@angular/common/http';
import { inject, Injectable, signal } from '@angular/core';
import { catchError, finalize, Observable, shareReplay, tap, throwError } from 'rxjs';

import { LoginRequest, LoginResponse, UserRole } from './auth.models';

@Injectable({ providedIn: 'root' })
export class AuthService {
  private readonly http = inject(HttpClient);
  private readonly tokenState = signal<string | null>(null);
  private readonly roleState = signal<UserRole | null>(null);
  private refreshInFlight: Observable<LoginResponse> | null = null;

  readonly accessToken = this.tokenState.asReadonly();
  readonly role = this.roleState.asReadonly();

  login(credentials: LoginRequest): Observable<LoginResponse> {
    return this.http.post<LoginResponse>('/api/auth/login', credentials, { withCredentials: true }).pipe(
      tap((response) => this.setSession(response))
    );
  }

  refresh(): Observable<LoginResponse> {
    if (!this.refreshInFlight) {
      this.refreshInFlight = this.http.post<LoginResponse>('/api/auth/refresh', {}, { withCredentials: true }).pipe(
        tap((response) => this.setSession(response)),
        catchError((error: unknown) => {
          this.clearSession();
          return throwError(() => error);
        }),
        finalize(() => {
          this.refreshInFlight = null;
        }),
        shareReplay({ bufferSize: 1, refCount: false })
      );
    }

    return this.refreshInFlight;
  }

  logout(): Observable<void> {
    return this.http.post<void>('/api/auth/logout', {}, { withCredentials: true }).pipe(
      finalize(() => this.clearSession())
    );
  }

  clearSession(): void {
    this.tokenState.set(null);
    this.roleState.set(null);
  }

  private setSession(response: LoginResponse): void {
    this.tokenState.set(response.access_token);
    this.roleState.set(response.role);
  }
}
