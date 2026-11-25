import { HttpClient, provideHttpClient, withInterceptors } from '@angular/common/http';
import { provideHttpClientTesting, HttpTestingController } from '@angular/common/http/testing';
import { TestBed } from '@angular/core/testing';

import { AuthService } from './auth.service';
import { authInterceptor } from './auth.interceptor';

describe('authInterceptor', () => {
  let http: HttpClient;
  let httpTesting: HttpTestingController;
  let auth: AuthService;

  beforeEach(() => {
    TestBed.configureTestingModule({
      providers: [
        provideHttpClient(withInterceptors([authInterceptor])),
        provideHttpClientTesting()
      ]
    });

    http = TestBed.inject(HttpClient);
    httpTesting = TestBed.inject(HttpTestingController);
    auth = TestBed.inject(AuthService);
  });

  afterEach(() => httpTesting.verify());

  it('shares one refresh between concurrent 401 responses and retries both requests', () => {
    auth.login({ email: 'agent@example.com', password: 'secret' }).subscribe();
    const login = httpTesting.expectOne('/api/auth/login');
    expect(login.request.withCredentials).toBeTrue();
    login.flush({
      access_token: 'old-access-token',
      token_type: 'Bearer',
      expires_in: 900,
      role: 'agent'
    });

    const results: string[] = [];
    http.get<{ result: string }>('/api/tickets/1').subscribe((response) => results.push(response.result));
    http.get<{ result: string }>('/api/tickets/2').subscribe((response) => results.push(response.result));

    const expiredRequests = httpTesting.match((request) => request.url.startsWith('/api/tickets/'));
    expect(expiredRequests.length).toBe(2);
    expiredRequests.forEach((request) => {
      expect(request.request.headers.get('Authorization')).toBe('Bearer old-access-token');
      request.flush({ error: 'expired' }, { status: 401, statusText: 'Unauthorized' });
    });

    const refresh = httpTesting.expectOne('/api/auth/refresh');
    expect(refresh.request.withCredentials).toBeTrue();
    expect(refresh.request.headers.has('Authorization')).toBeFalse();
    httpTesting.expectNone('/api/auth/refresh');
    refresh.flush({
      access_token: 'new-access-token',
      token_type: 'Bearer',
      expires_in: 900,
      role: 'agent'
    });

    const retriedRequests = httpTesting.match((request) => request.url.startsWith('/api/tickets/'));
    expect(retriedRequests.length).toBe(2);
    retriedRequests.forEach((request) => {
      expect(request.request.headers.get('Authorization')).toBe('Bearer new-access-token');
      request.flush({ result: 'ok' });
    });

    expect(results).toEqual(['ok', 'ok']);
  });
});
