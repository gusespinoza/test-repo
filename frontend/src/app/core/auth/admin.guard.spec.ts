import { TestBed } from '@angular/core/testing';
import { provideRouter, Router, UrlTree } from '@angular/router';
import { signal, WritableSignal } from '@angular/core';

import { AdminGuard } from './admin.guard';
import { AuthService } from './auth.service';
import { UserRole } from './auth.models';

describe('AdminGuard', () => {
  let role: WritableSignal<UserRole | null>;

  beforeEach(() => {
    role = signal<UserRole | null>('requester');
    TestBed.configureTestingModule({
      providers: [
        provideRouter([]),
        AdminGuard,
        { provide: AuthService, useValue: { role: role.asReadonly() } }
      ]
    });
  });

  it('allows admins', () => {
    role.set('admin');

    expect(TestBed.inject(AdminGuard).canActivate()).toBeTrue();
  });

  it('redirects non-admin users to events', () => {
    const result = TestBed.inject(AdminGuard).canActivate();

    expect(result instanceof UrlTree).toBeTrue();
    if (result instanceof UrlTree) {
      expect(TestBed.inject(Router).serializeUrl(result)).toBe('/events');
    }
  });
});
