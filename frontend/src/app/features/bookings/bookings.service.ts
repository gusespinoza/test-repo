import { HttpClient, HttpParams } from '@angular/common/http';
import { inject, Injectable } from '@angular/core';
import { Observable } from 'rxjs';

import { AdminBookingPage, CreateBookingRequest, Order } from './bookings.models';

@Injectable({ providedIn: 'root' })
export class BookingsService {
  private readonly http = inject(HttpClient);
  private readonly endpoint = '/api/bookings';

  createBooking(input: CreateBookingRequest): Observable<Order> {
    return this.http.post<Order>(this.endpoint, input);
  }

  getBooking(id: string): Observable<Order> {
    return this.http.get<Order>(`${this.endpoint}/${encodeURIComponent(id)}`);
  }

  listAdminBookings(page = 1, limit = 20): Observable<AdminBookingPage> {
    const params = new HttpParams().set('page', page).set('limit', limit);
    return this.http.get<AdminBookingPage>('/api/admin/bookings', { params });
  }

  confirmBooking(id: string): Observable<Order> {
    return this.http.post<Order>(`${this.endpoint}/${encodeURIComponent(id)}/confirm`, {});
  }

  cancelBooking(id: string): Observable<Order> {
    return this.http.delete<Order>(`${this.endpoint}/${encodeURIComponent(id)}`);
  }
}
