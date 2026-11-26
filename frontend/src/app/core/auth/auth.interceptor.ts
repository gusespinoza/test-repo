import { HttpErrorResponse, HttpInterceptorFn } from '@angular/common/http';
import { inject } from '@angular/core';
import { Router } from '@angular/router';
import { catchError, switchMap, throwError } from 'rxjs';

import { AuthService } from './auth.service';

export const authInterceptor: HttpInterceptorFn = (request, next) => {
  const auth = inject(AuthService);
  const router = inject(Router);
  const endpoint = request.url.split('?')[0];

  if (
    !request.url.startsWith('/api/') ||
    endpoint === '/api/auth/login' ||
    endpoint === '/api/auth/refresh'
  ) {
    return next(request);
  }

  const token = auth.accessToken();
  const authorizedRequest =
    token && !request.headers.has('Authorization')
      ? request.clone({ setHeaders: { Authorization: `Bearer ${token}` } })
      : request;

  return next(authorizedRequest).pipe(
    catchError((error: unknown) => {
      if (
        !(error instanceof HttpErrorResponse) ||
        error.status !== 401
      ) {
        return throwError(() => error);
      }

      const currentToken = auth.accessToken();
      if (
        currentToken &&
        authorizedRequest.headers.get('Authorization') !== `Bearer ${currentToken}`
      ) {
        return next(
          request.clone({
            setHeaders: { Authorization: `Bearer ${currentToken}` }
          })
        );
      }

      return auth.refresh().pipe(
        switchMap(() => {
          const refreshedToken = auth.accessToken();
          if (!refreshedToken) {
            return throwError(() => error);
          }
          return next(
            request.clone({
              setHeaders: { Authorization: `Bearer ${refreshedToken}` }
            })
          );
        }),
        catchError((refreshError: unknown) => {
          auth.clearSession();
          void router.navigateByUrl('/events');
          return throwError(() => refreshError);
        })
      );
    })
  );
};
